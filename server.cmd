@echo off
setlocal
set "PROJECT_ROOT=%~dp0."
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%PROJECT_ROOT%\deployments\setup-windows-server.ps1" -SourceRoot "%PROJECT_ROOT%" %*
exit /b %ERRORLEVEL%
