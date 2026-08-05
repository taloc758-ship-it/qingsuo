# 青梭 QingSuo

轻量的本地智能代理控制台，基于 sing-box 自动代理组运行。

## Prerequisites

- Node.js 22+ (installed)
- Go 1.26+ (a project-local copy is included in `../.tools/go` for this workspace)
- A `sing-box.exe` binary placed at `backend/data/bin/sing-box.exe`, or set `SINGBOX_BINARY` to its full path

## Run in development

Open two PowerShell terminals from this directory:

```powershell
$env:PATH = "D:\v2rayN-master\.tools\go\go\bin;$env:PATH"
cd backend
go run .
```

```powershell
cd frontend
npm install
npm run dev
```

Open the Vite address shown in the second terminal. The page proxies `/api` requests to the Go service at `http://127.0.0.1:8787`.

## What the first version does

- Stores an editable sing-box configuration in `backend/data/config.json`.
- Creates SOCKS (`127.0.0.1:2080`) and HTTP (`127.0.0.1:2081`) proxies with direct routing by default, so it can start before nodes are added.
- Starts and stops one local sing-box process.
- Exposes status, configuration, log, start, and stop endpoints for the React UI.

The default configuration has no proxy nodes and therefore routes directly. Once nodes are available, replace the direct outbound with a `urltest` outbound that lists their tags, then set `route.final` to that `urltest` tag.
