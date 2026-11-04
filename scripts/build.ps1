param([string]$Version = "0.1.0")
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
New-Item -ItemType Directory -Force dist | Out-Null
foreach ($arch in @("amd64", "arm64")) {
  $env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = $arch
  go build -trimpath -ldflags "-s -w" -o "dist/familytunnel-linux-$arch" ./cmd/familytunnel
}
$sumLines = foreach ($name in @("familytunnel-linux-amd64", "familytunnel-linux-arm64")) {
  $hash = (Get-FileHash "dist/$name" -Algorithm SHA256).Hash.ToLowerInvariant()
  "$hash  $name"
}
Set-Content -Path dist/SHA256SUMS -Value $sumLines -Encoding ascii
Set-Content -Path dist/release.json -Value ('{"version":"' + $Version + '"}') -Encoding ascii
tar -C dist -czf "dist/familytunnel-$Version-linux.tar.gz" familytunnel-linux-amd64 familytunnel-linux-arm64 release.json
$updateHash = (Get-FileHash "dist/familytunnel-$Version-linux.tar.gz" -Algorithm SHA256).Hash.ToLowerInvariant()
Set-Content -Path "dist/familytunnel-$Version-linux.tar.gz.sha256" -Value "$updateHash  familytunnel-$Version-linux.tar.gz" -Encoding ascii
$stage = Join-Path ([System.IO.Path]::GetTempPath()) ("familytunnel-stage-" + [guid]::NewGuid())
try {
  New-Item -ItemType Directory -Force "$stage/familytunnel/dist" | Out-Null
  Copy-Item install.sh,install-server.sh,install-client.sh,bootstrap.sh,README.md,TESTING.md -Destination "$stage/familytunnel"
  Copy-Item systemd,scripts -Destination "$stage/familytunnel" -Recurse
  Copy-Item dist/familytunnel-linux-amd64,dist/familytunnel-linux-arm64,dist/SHA256SUMS -Destination "$stage/familytunnel/dist"
  tar -C $stage -czf "dist/familytunnel-$Version-install.tar.gz" familytunnel
} finally { Remove-Item -LiteralPath $stage -Recurse -Force }
$installHash = (Get-FileHash "dist/familytunnel-$Version-install.tar.gz" -Algorithm SHA256).Hash.ToLowerInvariant()
Set-Content -Path "dist/familytunnel-$Version-install.tar.gz.sha256" -Value "$installHash  familytunnel-$Version-install.tar.gz" -Encoding ascii
