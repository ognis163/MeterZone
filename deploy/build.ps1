# Сборка бинарника для Keenetic (aarch64) и подготовка каталога dist.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

$env:GOOS = 'linux'
$env:GOARCH = 'arm64'
$env:CGO_ENABLED = '0'

Write-Host 'Сборка linux/arm64...' -ForegroundColor Cyan
go build -trimpath -ldflags '-s -w' -o meterzone .

$dist = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Force $dist | Out-Null
Copy-Item .\meterzone $dist\meterzone -Force
Copy-Item .\deploy\S99meterzone $dist\S99meterzone -Force

$f = Get-Item $dist\meterzone
Write-Host ("OK: {0} ({1:N2} МБ)" -f $f.Name, ($f.Length / 1MB)) -ForegroundColor Green
Write-Host 'Дальше: поднимите HTTP-сервер на каталоге deploy\dist (например, python -m http.server 18080) и выполните команды из INSTALL_Keenetic.md'
