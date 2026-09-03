@echo off
setlocal
cd /d "%~dp0"

if not exist "cursor-admin.exe" (
  echo [ERROR] cursor-admin.exe not found
  echo Build first: go build -o cursor-admin.exe .
  pause
  exit /b 1
)

tasklist /FI "IMAGENAME eq cursor-admin.exe" 2>nul | find /I "cursor-admin.exe" >nul
if %ERRORLEVEL%==0 (
  echo [OK] already running
  echo Open http://localhost:9999
  start "" "http://localhost:9999"
  exit /b 0
)

echo [START] cursor-admin.exe ...
start "cursor-admin" /D "%~dp0" /MIN cmd /c "cursor-admin.exe >> cursor-admin.log 2>&1"

timeout /t 2 /nobreak >nul

tasklist /FI "IMAGENAME eq cursor-admin.exe" 2>nul | find /I "cursor-admin.exe" >nul
if %ERRORLEVEL%==0 (
  echo [OK] started
  echo URL: http://localhost:9999
  echo Log: %cd%\cursor-admin.log
  start "" "http://localhost:9999"
  exit /b 0
)

echo [FAIL] process not running. Last log lines:
echo ----------------------------------------
powershell -NoProfile -Command "if (Test-Path -LiteralPath 'cursor-admin.log') { Get-Content -LiteralPath 'cursor-admin.log' -Tail 20 } else { 'no log file' }"
echo ----------------------------------------
pause
exit /b 1
