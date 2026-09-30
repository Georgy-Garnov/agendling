# Builds bin\agendling.exe natively on Windows (Go 1.26+; no C compiler needed).
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$env:CGO_ENABLED = "0"
$version = (git describe --tags --always --dirty 2>$null)
if (-not $version) { $version = "dev" }
$winver = "0.0.0.0"
if ($version -match '^v?(\d+)\.(\d+)\.(\d+)') { $winver = "$($Matches[1]).$($Matches[2]).$($Matches[3]).0" }
go run github.com/tc-hib/go-winres@v0.3.3 make --in cmd\agendling\winres\winres.json --out cmd\agendling\rsrc --arch amd64 --product-version $winver --file-version $winver
New-Item -ItemType Directory -Force bin | Out-Null
go build -trimpath -ldflags "-H windowsgui -s -w -X github.com/Georgy-Garnov/agendling/internal/appinfo.Version=$version" -o bin\agendling.exe .\cmd\agendling
Write-Host "Built bin\agendling.exe ($version, file version $winver)"
