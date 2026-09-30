# Builds bin\agendling.exe natively on Windows (Go 1.26+; no C compiler needed).
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$env:CGO_ENABLED = "0"
$version = (git describe --tags --always --dirty 2>$null)
if (-not $version) { $version = "dev" }
if (-not (Test-Path cmd\agendling\rsrc_windows_amd64.syso)) {
    go run github.com/akavel/rsrc@v0.10.2 -manifest cmd\agendling\agendling.manifest -arch amd64 -o cmd\agendling\rsrc_windows_amd64.syso
}
New-Item -ItemType Directory -Force bin | Out-Null
go build -trimpath -ldflags "-H windowsgui -s -w -X github.com/Georgy-Garnov/agendling/internal/appinfo.Version=$version" -o bin\agendling.exe .\cmd\agendling
Write-Host "Built bin\agendling.exe ($version)"
