$ErrorActionPreference = "Stop"

$Repo = "moKshagna-p/letterboxd-TUI-Heatmap"
$InstallDir = if ($env:LETTERBOXD_TUI_INSTALL_DIR) { $env:LETTERBOXD_TUI_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\letterboxd-tui" }

$arch = switch ($env:PROCESSOR_ARCHITECTURE.ToLowerInvariant()) {
  "amd64" { "amd64" }
  "arm64" { "arm64" }
  default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$zipName = "letterboxd-tui_windows_${arch}.zip"
$zipUrl = "https://github.com/$Repo/releases/latest/download/$zipName"

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("letterboxd-tui-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmpDir | Out-Null

try {
  $zipPath = Join-Path $tmpDir $zipName
  Invoke-WebRequest -Uri $zipUrl -OutFile $zipPath

  if (Test-Path $InstallDir) {
    Remove-Item -Recurse -Force $InstallDir
  }
  New-Item -ItemType Directory -Path $InstallDir | Out-Null
  Expand-Archive -Path $zipPath -DestinationPath $InstallDir -Force

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (-not ($userPath -split ";" | Where-Object { $_ -eq $InstallDir })) {
    $newPath = if ([string]::IsNullOrWhiteSpace($userPath)) { $InstallDir } else { "$userPath;$InstallDir" }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
  }

  Write-Host "installed letterboxd-tui to $InstallDir"
  Write-Host "open a new terminal and run: letterboxd-tui"
}
finally {
  if (Test-Path $tmpDir) {
    Remove-Item -Recurse -Force $tmpDir
  }
}
