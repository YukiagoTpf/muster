# Installs the latest Muster release on Windows; run it again to upgrade.
#   irm https://raw.githubusercontent.com/YukiagoTpf/muster/main/install.ps1 | iex
# MUSTER_INSTALL_DIR picks the directory (default %LOCALAPPDATA%\Programs\muster).
$ErrorActionPreference = 'Stop'

$dir = if ($env:MUSTER_INSTALL_DIR) { $env:MUSTER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\muster' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "muster_windows_$arch.exe"
$url = 'https://github.com/YukiagoTpf/muster/releases/latest/download'

New-Item -ItemType Directory -Force $dir | Out-Null
$exe = Join-Path $dir 'muster.exe'
$download = "$exe.download"
$sums = "$exe.checksums"
Invoke-WebRequest -UseBasicParsing "$url/$asset" -OutFile $download
Invoke-WebRequest -UseBasicParsing "$url/checksums.txt" -OutFile $sums
$want = (Get-Content $sums | Where-Object { $_ -match " $([regex]::Escape($asset))$" }) -replace ' .*', ''
Remove-Item $sums
if ((Get-FileHash $download -Algorithm SHA256).Hash -ne $want) {
  Remove-Item $download
  throw "muster: checksum mismatch for $asset"
}

# A running muster.exe can't be overwritten but can be renamed, so an upgrade
# moves the old one aside (muster update can upgrade Muster itself).
Remove-Item "$exe.old" -ErrorAction SilentlyContinue
if (Test-Path $exe) { Move-Item -Force $exe "$exe.old" }
Move-Item $download $exe
Write-Host "installed muster $(& $exe version) to $exe"

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
  [Environment]::SetEnvironmentVariable('Path', "$($userPath.TrimEnd(';'));$dir", 'User')
  $env:Path = "$env:Path;$dir"
  Write-Host "added $dir to your PATH"
}
