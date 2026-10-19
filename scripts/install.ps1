# Windows installer (PowerShell):  irm https://raw.githubusercontent.com/yeongseoo/tchat/main/scripts/install.ps1 | iex
$ErrorActionPreference = "Stop"
$repo = "yeongseoo/tchat"
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$url = "https://github.com/$repo/releases/latest/download/tchat_windows_$arch.zip"
$dir = Join-Path $env:LOCALAPPDATA "tchat"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$zip = Join-Path $env:TEMP "tchat.zip"
Write-Host "다운로드: $url"
Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
Expand-Archive -Path $zip -DestinationPath $dir -Force
Remove-Item $zip
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not ($userPath -split ";" | Where-Object { $_ -eq $dir })) {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
  $env:Path = "$env:Path;$dir"
}
Write-Host "설치 완료! 새 터미널 창을 열고 'tchat' 을 입력하세요."
