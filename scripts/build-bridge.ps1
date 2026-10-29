<# Build dist\Nodelight.exe (needs Go 1.26+). It is a windowless (GUI subsystem) program: no console opens. #>
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$out  = Join-Path $root 'dist'
New-Item -ItemType Directory -Force $out | Out-Null

Push-Location (Join-Path $root 'bridge')
try {
    # Resolves tailscale.com and writes go.sum (not committed by the template).
    go mod tidy
    if ($LASTEXITCODE -ne 0) { throw 'go mod tidy failed' }
    go test ./cmd/...
    if ($LASTEXITCODE -ne 0) { throw 'tests failed' }
    # relay/ is its own Go module, so it is tested from its own directory.
    Push-Location relay
    try { go test -count=1 ./...; if ($LASTEXITCODE -ne 0) { throw 'relay tests failed' } } finally { Pop-Location }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    go build -tags=ts_omit_logtail -trimpath -ldflags='-s -w -H=windowsgui' -o (Join-Path $out 'Nodelight.exe') ./cmd/nodelight
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally { Pop-Location }
Write-Host "Built $out\Nodelight.exe"
