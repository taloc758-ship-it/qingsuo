param(
    [string]$OutputDirectory = "release"
)

$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$frontendDirectory = Join-Path $projectRoot "frontend"
$backendDirectory = Join-Path $projectRoot "backend"
$projectParent = Split-Path -Parent $projectRoot
$goExe = Join-Path $projectParent ".tools\go\go\bin\go.exe"
$gofmtExe = Join-Path $projectParent ".tools\go\go\bin\gofmt.exe"

if (-not (Test-Path -LiteralPath $goExe)) {
    $goExe = (Get-Command go -ErrorAction Stop).Source
    $gofmtExe = Join-Path (Split-Path -Parent $goExe) "gofmt.exe"
}

$releaseDirectory = [System.IO.Path]::GetFullPath((Join-Path $projectRoot $OutputDirectory))
$projectRootWithSeparator = $projectRoot.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
if (-not $releaseDirectory.StartsWith($projectRootWithSeparator, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "OutputDirectory must stay inside the project directory."
}
$stagingDirectory = Join-Path $projectRoot ".package-staging"
$embeddedWebDirectory = Join-Path $backendDirectory "web\dist"
$frontendDistDirectory = Join-Path $frontendDirectory "dist"

if (Test-Path -LiteralPath $stagingDirectory) {
    Remove-Item -LiteralPath $stagingDirectory -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $stagingDirectory | Out-Null

Push-Location $frontendDirectory
try {
    & npm run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed." }
} finally {
    Pop-Location
}

if (Test-Path -LiteralPath $embeddedWebDirectory) {
    Remove-Item -LiteralPath $embeddedWebDirectory -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $embeddedWebDirectory | Out-Null
Copy-Item -Path (Join-Path $frontendDistDirectory "*") -Destination $embeddedWebDirectory -Recurse -Force

Push-Location $backendDirectory
try {
    & $gofmtExe -w main.go main_test.go
    & $goExe test ./...
    if ($LASTEXITCODE -ne 0) { throw "Backend tests failed." }
    & $goExe build -ldflags "-X main.packagedBuild=true" -o (Join-Path $stagingDirectory "青梭.exe") .
    if ($LASTEXITCODE -ne 0) { throw "Backend build failed." }
} finally {
    Pop-Location
}

$dataDirectory = Join-Path $stagingDirectory "data"
New-Item -ItemType Directory -Force -Path $dataDirectory | Out-Null
Copy-Item -LiteralPath (Join-Path $backendDirectory "data\bin") -Destination $dataDirectory -Recurse -Force
Copy-Item -LiteralPath (Join-Path $backendDirectory "data\srss") -Destination $dataDirectory -Recurse -Force

@'
双击“启动青梭.cmd”即可打开控制台。

首次运行不带任何订阅；请自行导入订阅链接。
程序仅支持 Windows 10/11 x64，不需要安装 Go 或 Node.js。
'@ | Set-Content -LiteralPath (Join-Path $stagingDirectory "使用说明.txt") -Encoding utf8

@'
@echo off
cd /d "%~dp0"
start "" "%~dp0青梭.exe" --open
'@ | Set-Content -LiteralPath (Join-Path $stagingDirectory "启动青梭.cmd") -Encoding ascii

if (Test-Path -LiteralPath $releaseDirectory) {
    Remove-Item -LiteralPath $releaseDirectory -Recurse -Force
}
Move-Item -LiteralPath $stagingDirectory -Destination $releaseDirectory

$zipPath = "$releaseDirectory.zip"
if (Test-Path -LiteralPath $zipPath) {
    Remove-Item -LiteralPath $zipPath -Force
}
Compress-Archive -LiteralPath $releaseDirectory -DestinationPath $zipPath -CompressionLevel Optimal

Write-Host "Portable package created: $releaseDirectory"
Write-Host "Portable ZIP created: $zipPath"
