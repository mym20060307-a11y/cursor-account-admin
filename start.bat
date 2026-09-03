@echo off
setlocal
cd /d "%~dp0"

if not exist "cursor-account-admin.exe" (
  echo [ERROR] cursor-account-admin.exe not found
  echo Build first: go build -o cursor-account-admin.exe .
  pause
  exit /b 1
)

tasklist /FI "IMAGENAME eq cursor-account-admin.exe" 2>nul | find /I "cursor-account-admin.exe" >nul
if %ERRORLEVEL%==0 (
  echo [OK] already running
  echo Open http://localhost:9999
  start "" "http://localhost:9999"
  exit /b 0
)

echo [START] cursor-account-admin.exe ...
start "cursor-account-admin" /D "%~dp0" /MIN cmd /c "cursor-account-admin.exe >> cursor-account-admin.log 2>&1"

timeout /t 2 /nobreak >nul

tasklist /FI "IMAGENAME eq cursor-account-admin.exe" 2>nul | find /I "cursor-account-admin.exe" >nul
if %ERRORLEVEL%==0 (
  echo [OK] started
  echo URL: http://localhost:9999
  echo Log: %cd%\cursor-account-admin.log
  start "" "http://localhost:9999"
  exit /b 0
)

echo [FAIL] process not running. Last log lines:
echo ----------------------------------------
powershell -NoProfile -Command "if (Test-Path -LiteralPath 'cursor-account-admin.log') { Get-Content -LiteralPath 'cursor-account-admin.log' -Tail 20 } else { 'no log file' }"
echo ----------------------------------------
pause
exit /b 1
