# QingSuo

Windows desktop proxy console built around sing-box. It supports subscription groups,
node latency tests, automatic failover, route rules, Windows system-proxy control, and
a tray-resident desktop experience.

## Stack

- Desktop: Electron
- Frontend: React, TypeScript, Vite
- Backend: Go standard-library HTTP service
- Proxy core: sing-box
- Packaging: electron-builder and Go

## Prerequisites

- Node.js 22 or newer
- Go 1.26 or newer
- A Windows `sing-box.exe` placed at `backend\data\bin\sing-box.exe`

The sing-box executable is intentionally not committed. Download a compatible Windows
build from the official sing-box releases before packaging.

## Development

Start the backend:

```powershell
cd backend
go run .
```

In another terminal, start the frontend:

```powershell
cd frontend
npm install
npm run dev
```

## Build the desktop app

Run this from the project root:

```powershell
powershell -ExecutionPolicy Bypass -File .\package-electron.ps1
```

The folder build is created at `release-electron\青梭桌面版`. Launch `QingSuo.exe`.
The target computer does not need Go, Node.js, Python, or a separately started backend.

## Data and web resources

The app stores subscriptions, selected nodes, route rules, and automatic-switch settings
beside the executable:

```text
青梭桌面版\data\
```

The web frontend stays external to the executable at `resources\web`. Replacing its
`index.html` or files under `assets` takes effect after restarting the app.
