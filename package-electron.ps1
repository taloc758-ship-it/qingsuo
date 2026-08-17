param(
    [string]$OutputDirectory = "release-electron"
)

$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$frontendDirectory = Join-Path $projectRoot "frontend"
$backendDirectory = Join-Path $projectRoot "backend"
$desktopDirectory = Join-Path $projectRoot "desktop"
$projectParent = Split-Path -Parent $projectRoot
$goExe = Join-Path $projectParent ".tools\go\go\bin\go.exe"
$gofmtExe = Join-Path $projectParent ".tools\go\go\bin\gofmt.exe"

if (-not (Test-Path -LiteralPath $goExe)) {
    $legacyGoExe = "D:\v2rayN-master\.tools\go\go\bin\go.exe"
    if (Test-Path -LiteralPath $legacyGoExe) {
        $goExe = $legacyGoExe
        $gofmtExe = Join-Path (Split-Path -Parent $goExe) "gofmt.exe"
    } else {
        $goExe = (Get-Command go -ErrorAction Stop).Source
        $gofmtExe = Join-Path (Split-Path -Parent $goExe) "gofmt.exe"
    }
}

$releaseDirectory = [System.IO.Path]::GetFullPath((Join-Path $projectRoot $OutputDirectory))
$projectRootWithSeparator = $projectRoot.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
if (-not $releaseDirectory.StartsWith($projectRootWithSeparator, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "OutputDirectory must stay inside the project directory."
}

$stagingDirectory = Join-Path $projectRoot ".electron-staging"
$frontendDistDirectory = Join-Path $frontendDirectory "dist"
$iconPath = Join-Path $projectRoot "assets\icons\qingsuo-shield.ico"
$goCacheDirectory = Join-Path $projectRoot ".package-go-cache"

if (-not (Test-Path -LiteralPath $iconPath)) {
    & (Join-Path $projectRoot "tools\New-QingSuoIconVariants.ps1") -OutputDirectory (Split-Path -Parent $iconPath)
    if ($LASTEXITCODE -ne 0) { throw "Application icon generation failed." }
}

if (Test-Path -LiteralPath $stagingDirectory) { Remove-Item -LiteralPath $stagingDirectory -Recurse -Force }
if (Test-Path -LiteralPath $releaseDirectory) { Remove-Item -LiteralPath $releaseDirectory -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stagingDirectory | Out-Null
if (Test-Path -LiteralPath $goCacheDirectory) { Remove-Item -LiteralPath $goCacheDirectory -Recurse -Force }
New-Item -ItemType Directory -Force -Path $goCacheDirectory | Out-Null
$env:GOCACHE = $goCacheDirectory

Push-Location $frontendDirectory
try {
    & npm run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed." }
} finally { Pop-Location }

$stagedWebDirectory = Join-Path $stagingDirectory "web"
New-Item -ItemType Directory -Force -Path $stagedWebDirectory | Out-Null
Copy-Item -Path (Join-Path $frontendDistDirectory "*") -Destination $stagedWebDirectory -Recurse -Force

$stagedBackend = Join-Path $stagingDirectory "backend"
New-Item -ItemType Directory -Force -Path $stagedBackend | Out-Null
Push-Location $backendDirectory
try {
    & $gofmtExe -w main.go main_test.go
    & $goExe test ./...
    if ($LASTEXITCODE -ne 0) { throw "Backend tests failed." }
    & $goExe build -ldflags "-H=windowsgui -X main.packagedBuild=true" -o (Join-Path $stagedBackend "qingsuo-backend.exe") .
    if ($LASTEXITCODE -ne 0) { throw "Backend build failed." }
} finally { Pop-Location }
Remove-Item -LiteralPath $goCacheDirectory -Recurse -Force -ErrorAction SilentlyContinue

# Current settings are bundled as both fallback seed data and the initial
# portable data directory beside the desktop executable.
$stagedDataDirectory = Join-Path $stagedBackend "data"
if (Test-Path -LiteralPath $stagedDataDirectory) { Remove-Item -LiteralPath $stagedDataDirectory -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stagedDataDirectory | Out-Null
Copy-Item -Path (Join-Path $backendDirectory "data\*") -Destination $stagedDataDirectory -Recurse -Force
Get-ChildItem -LiteralPath $stagedDataDirectory -Recurse -File -Filter "*.log" | Remove-Item -Force

foreach ($requiredFile in "config.json", "subscriptions.json", "whitelist.json", "routing.json", "tun.json", "auto-switch.json", "failed-node-cleanup.json") {
    $sourceFile = Join-Path $backendDirectory "data\$requiredFile"
    $stagedFile = Join-Path $stagedDataDirectory $requiredFile
    if (-not (Test-Path -LiteralPath $sourceFile) -or -not (Test-Path -LiteralPath $stagedFile)) {
        throw "Required current configuration is missing: $requiredFile"
    }
    if ((Get-FileHash -LiteralPath $sourceFile).Hash -ne (Get-FileHash -LiteralPath $stagedFile).Hash) {
        throw "Staged configuration does not match current data: $requiredFile"
    }
}

Push-Location $desktopDirectory
try {
    if (-not (Test-Path -LiteralPath (Join-Path $desktopDirectory "node_modules\electron"))) {
        & npm install
        if ($LASTEXITCODE -ne 0) { throw "Electron dependencies installation failed." }
    }
    # electron-builder defaults to the package.json output directory. Override
    # it here so OutputDirectory controls both the builder output and handoff.
    $builder = Join-Path $desktopDirectory "node_modules\.bin\electron-builder.cmd"
    & $builder --win dir --x64 "--config.directories.output=$releaseDirectory"
    if ($LASTEXITCODE -ne 0) { throw "Electron packaging failed." }
} finally { Pop-Location }

$unpackedDirectory = Join-Path $releaseDirectory "win-unpacked"
$desktopAppDirectory = Join-Path $releaseDirectory "青梭桌面版"
if (-not (Test-Path -LiteralPath $unpackedDirectory)) {
    throw "Electron application folder was not created."
}

# Ship current settings beside the desktop executable. This avoids the
# temporary extraction behavior of Electron single-file portable packages.
$runtimeDataDirectory = Join-Path $unpackedDirectory "data"
New-Item -ItemType Directory -Force -Path $runtimeDataDirectory | Out-Null
Copy-Item -Path (Join-Path $stagedDataDirectory "*") -Destination $runtimeDataDirectory -Recurse -Force
foreach ($requiredFile in "config.json", "subscriptions.json", "whitelist.json", "routing.json", "tun.json", "auto-switch.json", "failed-node-cleanup.json") {
    $sourceFile = Join-Path $backendDirectory "data\$requiredFile"
    $runtimeFile = Join-Path $runtimeDataDirectory $requiredFile
    if ((Get-FileHash -LiteralPath $sourceFile).Hash -ne (Get-FileHash -LiteralPath $runtimeFile).Hash) {
        throw "Desktop runtime configuration does not match current data: $requiredFile"
    }
}
Move-Item -LiteralPath $unpackedDirectory -Destination $desktopAppDirectory

@'
青梭 QingSuo 桌面版
====================

使用方式
--------
1. 双击 QingSuo.exe。程序会启动代理核心并打开控制台。
2. 关闭窗口会隐藏到右下角的系统托盘，代理继续运行；左键托盘图标可恢复窗口。
3. 需要完全退出时，右键托盘图标并选择“退出”。退出会停止代理核心，并关闭由青梭开启的 Windows 系统代理。
4. “全局代理”开启后，所有接入青梭的流量都会走当前代理节点；关闭后恢复大陆和自定义白名单直连。
5. TUN 模式可接管 Navicat 等不遵循 Windows 系统代理的应用流量。开启 TUN 前，请右键 QingSuo.exe 并选择“以管理员身份运行”。
6. 界面中的“重启”只重启代理核心、重新加载配置，不会修改系统代理开关。
7. 可在“路由规则”区域打开“开机自启动”；Windows 登录后，青梭会启动并驻留托盘。

数据与迁移
----------
当前订阅、节点选择、路由白名单和自动切换设置均保存在本目录的 data 文件夹。
迁移到另一台电脑时，请完整复制整个“青梭桌面版”文件夹。订阅链接可能带有访问令牌，不要把 data 文件夹发给不可信的人。

开发技术
--------
桌面壳：Electron。负责原生窗口、无系统标题栏、系统托盘、启动/退出本地服务和打包为 Windows 程序。
前端：React + TypeScript + Vite。提供订阅管理、节点列表、测速、自动切换、路由规则和日志界面。
后端：Go 标准库 HTTP 服务。负责订阅解析、配置生成、节点状态、测速、系统代理与持久化配置。
代理核心：sing-box。实际建立代理连接、分流规则和 URL 延迟测试。
打包：electron-builder + Go 编译。目标电脑无需安装 Go、Node.js、Python 或额外后端环境。

目录说明
--------
QingSuo.exe        桌面程序入口
data\              用户配置和订阅数据；请保留
resources\web\     前端页面资源；替换此处的 index.html / assets 后重启可生效
resources\backend\ 内置 Go 后端及运行资源；请勿单独删除或移动
'@ | Set-Content -LiteralPath (Join-Path $desktopAppDirectory "使用说明.txt") -Encoding utf8

Write-Host "Electron portable application created: $releaseDirectory"
