@echo off
chcp 65001 >nul
where pwsh.exe >nul 2>nul
if %errorlevel%==0 (
  pwsh.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -NoExit -File "%~dp0start_easy_outlook.ps1"
) else (
  powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -NoExit -File "%~dp0start_easy_outlook.ps1"
)
