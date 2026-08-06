const { contextBridge, ipcRenderer } = require("electron");

// Keep the renderer isolated: only expose the three native window actions it needs.
contextBridge.exposeInMainWorld("qingSuoWindow", {
  minimize: () => ipcRenderer.invoke("window:minimize"),
  toggleMaximize: () => ipcRenderer.invoke("window:toggle-maximize"),
  hide: () => ipcRenderer.invoke("window:hide")
});
