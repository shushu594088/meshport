$ErrorActionPreference='Stop'
$releaseVersion='0.1.0'
$projectRoot=Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
$goBinary=(Get-Command go -ErrorAction SilentlyContinue).Source
if (!$goBinary) { $goBinary=Join-Path $env:LOCALAPPDATA 'meshport-build/go/bin/go.exe' }
function Check-Exit { if ($LASTEXITCODE -ne 0) {throw "Command failed: $LASTEXITCODE"} }
npm --prefix web ci
Check-Exit
npm --prefix web run build
Check-Exit
& $goBinary test ./...
Check-Exit
$env:CGO_ENABLED='0'
$env:GOOS='linux'
foreach ($architecture in @('amd64','arm64')) {
 $env:GOARCH=$architecture
 $releasePath=Join-Path $projectRoot "build/meshport-linux-$architecture"
 New-Item -ItemType Directory -Path $releasePath -Force | Out-Null
 & $goBinary build -trimpath -ldflags '-s -w' -o "$releasePath/meshport" ./cmd/meshport
 Check-Exit
 & $goBinary build -trimpath -ldflags '-s -w' -o "$releasePath/meshport-helper" ./cmd/meshport-helper
 Check-Exit
 Copy-Item -LiteralPath scripts/install.sh,scripts/uninstall.sh,deploy/meshport.service,deploy/meshport-mihomo.service -Destination $releasePath
 Copy-Item -LiteralPath README.md,THIRD_PARTY_NOTICES.txt -Destination $releasePath
 Copy-Item -LiteralPath docs -Destination $releasePath -Recurse -Force
 [IO.File]::WriteAllText((Join-Path $releasePath 'ARCH'),"$architecture`n",[Text.UTF8Encoding]::new($false))
 tar -czf "build/meshport-0.1.0-linux-$architecture.tar.gz" -C $releasePath .
 Check-Exit
}
Get-ChildItem "build/meshport-$releaseVersion-linux-*.tar.gz" | Sort-Object Name | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Hash.ToLower())  $([IO.Path]::GetFileName($_.Path))" } | Set-Content -Encoding ascii build/SHA256SUMS
Copy-Item -LiteralPath deploy/install-targets.tsv -Destination build/INSTALL-TARGETS.tsv
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
