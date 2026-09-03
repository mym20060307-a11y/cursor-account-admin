@echo off
cd /d "%~dp0"

tasklist /FI "IMAGENAME eq cursor-account-admin.exe" 2>nul | find /I "cursor-account-admin.exe" >nul
if not %ERRORLEVEL%==0 (
  echo [OK] not running
  exit /b 0
)

echo [STOP] killing cursor-account-admin.exe ...
taskkill /F /IM cursor-account-admin.exe >nul 2>&1
timeout /t 1 /nobreak >nul

tasklist /FI "IMAGENAME eq cursor-account-admin.exe" 2>nul | find /I "cursor-account-admin.exe" >nul
if %ERRORLEVEL%==0 (
  echo [FAIL] still running
  pause
  exit /b 1
)

echo [OK] stopped
exit /b 0
