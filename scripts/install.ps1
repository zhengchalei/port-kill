param([string]$Source)
$ErrorActionPreference = 'Stop'

if (-not $Source) {
    $releaseExecutable = Join-Path $PSScriptRoot 'PortPilot.exe'
    $projectExecutable = Join-Path (Split-Path $PSScriptRoot -Parent) 'build\PortPilot.exe'
    if (Test-Path -LiteralPath $releaseExecutable) {
        $Source = $releaseExecutable
    } elseif (Test-Path -LiteralPath $projectExecutable) {
        $Source = $projectExecutable
    } else {
        throw '找不到 PortPilot.exe，请先编译项目或解压下载的 Windows 版本。'
    }
}

$installDirectory = Join-Path $env:LOCALAPPDATA 'Programs\PortPilot'
$installedExecutable = Join-Path $installDirectory 'PortPilot.exe'
New-Item -ItemType Directory -Path $installDirectory -Force | Out-Null
Copy-Item -LiteralPath $Source -Destination $installedExecutable -Force

$desktopShortcut = Join-Path ([Environment]::GetFolderPath('Desktop')) 'PortPilot 端口管理器.lnk'
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($desktopShortcut)
$shortcut.TargetPath = $installedExecutable
$shortcut.WorkingDirectory = $installDirectory
$shortcut.Description = '查看本机端口占用进程，快速定位并结束占用端口的进程。'
$shortcut.IconLocation = "$installedExecutable,0"
$shortcut.Save()
Write-Host "已安装：$installedExecutable"
Write-Host "桌面快捷方式：$desktopShortcut"
