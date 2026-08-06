const { app, BrowserWindow, dialog, ipcMain, Menu, shell, Tray } = require("electron");
const { spawn } = require("child_process");
const fs = require("fs/promises");
const path = require("path");

const apiPort = 28888;
const settingsFiles = [
  "config.json",
  "subscriptions.json",
  "whitelist.json",
  "auto-switch.json",
  "failed-node-cleanup.json"
];
const initializationMarker = ".qingsuo-initialized.json";
let mainWindow;
let backend;
let tray;
let isQuitting = false;

// Used only by packaging tests; normal users keep settings in AppData.
if (process.env.QINGSUO_USER_DATA_DIR) {
  app.setPath("userData", process.env.QINGSUO_USER_DATA_DIR);
}

const hasSingleInstanceLock = app.requestSingleInstanceLock();
if (!hasSingleInstanceLock) {
  app.quit();
}

app.on("second-instance", () => {
  showMainWindow();
});

function resourcePath(...parts) {
  if (app.isPackaged) return path.join(process.resourcesPath, ...parts);
  return path.join(__dirname, "..", ".electron-staging", ...parts);
}

function portableDataPath() {
  // The desktop release keeps its user-owned settings beside the executable,
  // so copying the whole application folder also carries its configuration.
  return process.env.PORTABLE_EXECUTABLE_DIR || path.dirname(process.execPath);
}

async function pathExists(target) {
  try {
    await fs.access(target);
    return true;
  } catch {
    return false;
  }
}

async function hasNoSubscriptions(dataDirectory) {
  try {
    const content = await fs.readFile(path.join(dataDirectory, "subscriptions.json"), "utf8");
    const groups = JSON.parse(content);
    return Array.isArray(groups) && groups.length === 0;
  } catch {
    return true;
  }
}

async function copySeedSettings(seed, destination) {
  for (const file of settingsFiles) {
    const source = path.join(seed, file);
    if (await pathExists(source)) {
      await fs.copyFile(source, path.join(destination, file));
    }
  }
  await fs.writeFile(
    path.join(destination, initializationMarker),
    JSON.stringify({ seededAt: new Date().toISOString() })
  );
}

async function ensureDataDirectory() {
  const destination = path.join(portableDataPath(), "data");
  const seed = resourcePath("backend", "data");
  if (!(await pathExists(seed))) {
    throw new Error(`Missing bundled application data: ${seed}`);
  }
  if (!(await pathExists(destination))) {
    await fs.cp(seed, destination, { recursive: true });
    await copySeedSettings(seed, destination);
    return destination;
  }

  // Earlier desktop builds created a default empty data directory before the
  // bundled settings were copied. Recover only that known-empty first-run case.
  const marker = path.join(destination, initializationMarker);
  if (!(await pathExists(marker)) && !(await hasNoSubscriptions(seed))) {
    if (await hasNoSubscriptions(destination)) {
      await copySeedSettings(seed, destination);
      return destination;
    }
  }
  return destination;
}

async function waitForBackend() {
  const endpoint = `http://127.0.0.1:${apiPort}/api/status`;
  const deadline = Date.now() + 15000;
  let lastError;

  while (Date.now() < deadline) {
    try {
      const response = await fetch(endpoint, { signal: AbortSignal.timeout(1000) });
      if (response.ok) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`Local service did not start. ${lastError?.message ?? ""}`.trim());
}

async function startBackend() {
  const executable = resourcePath("backend", "qingsuo-backend.exe");
	const webDirectory = resourcePath("web");
  if (!(await pathExists(executable))) {
    throw new Error(`Missing bundled backend: ${executable}`);
  }
	if (!(await pathExists(path.join(webDirectory, "index.html")))) {
		throw new Error(`Missing bundled frontend: ${webDirectory}`);
	}

  const dataDirectory = await ensureDataDirectory();
  backend = spawn(executable, [], {
    windowsHide: true,
    stdio: "ignore",
    env: {
      ...process.env,
      SINGBOX_WEB_DATA_DIR: dataDirectory,
      SINGBOX_WEB_DIR: webDirectory,
      SINGBOX_WEB_LISTEN_PORT: String(apiPort),
      SINGBOX_WEB_OPEN_BROWSER: "false"
    }
  });
  backend.on("error", (error) => {
    if (!isQuitting) console.error("Backend process error:", error);
  });
  await waitForBackend();
  const response = await fetch(`http://127.0.0.1:${apiPort}/api/restart`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
    signal: AbortSignal.timeout(15000)
  });
  if (!response.ok) {
    const detail = (await response.text()).trim();
    throw new Error(`Unable to start sing-box. ${detail}`.trim());
  }
}

async function stopBackend() {
  if (!backend || backend.killed) return;
  try {
    await fetch(`http://127.0.0.1:${apiPort}/api/exit`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
      signal: AbortSignal.timeout(3000)
    });
  } catch {
    // The child may already have exited; killing it below is still safe.
  }
  backend.kill();
  backend = undefined;
}

function showMainWindow() {
  if (!mainWindow) return;
  if (mainWindow.isMinimized()) mainWindow.restore();
  mainWindow.show();
  mainWindow.focus();
}

function createTray() {
  tray = new Tray(resourcePath("qingsuo-sixfold.ico"));
  tray.setToolTip("青梭 QingSuo");
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: "显示青梭", click: showMainWindow },
    { type: "separator" },
    { label: "退出", click: () => app.quit() }
  ]));
  tray.on("click", showMainWindow);
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1220,
    height: 820,
    minWidth: 960,
    minHeight: 660,
    show: false,
    frame: false,
    icon: resourcePath("qingsuo-sixfold.ico"),
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      preload: path.join(__dirname, "preload.cjs")
    }
  });
  mainWindow.removeMenu();
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: "deny" };
  });
  mainWindow.on("close", (event) => {
    if (isQuitting) return;
    event.preventDefault();
    mainWindow.hide();
  });
  mainWindow.once("ready-to-show", () => mainWindow.show());
  mainWindow.loadURL(`http://127.0.0.1:${apiPort}/`);
}

ipcMain.handle("window:minimize", () => mainWindow?.minimize());
ipcMain.handle("window:toggle-maximize", () => {
  if (!mainWindow) return;
  if (mainWindow.isMaximized()) mainWindow.unmaximize();
  else mainWindow.maximize();
});
ipcMain.handle("window:hide", () => mainWindow?.hide());

app.whenReady().then(async () => {
  app.setAppUserModelId("com.qingsuo.desktop.sixfold");
  try {
    await startBackend();
    createWindow();
    createTray();
  } catch (error) {
    await dialog.showMessageBox({
      type: "error",
      title: "青梭启动失败",
      message: "本地服务无法启动。",
      detail: error.message
    });
    app.quit();
  }
});

app.on("before-quit", (event) => {
  if (isQuitting) return;
  isQuitting = true;
  event.preventDefault();
  stopBackend().finally(() => app.quit());
});

app.on("window-all-closed", (event) => event.preventDefault());
