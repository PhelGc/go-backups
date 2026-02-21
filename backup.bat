@echo off
setlocal

set CONFIG=all-dbs.yaml
set LOG_LEVEL=info

echo ============================================
echo  GoBackups - Backup completo
echo  %DATE% %TIME%
echo ============================================
echo.

REM Cambiar al directorio donde esta el bat
cd /d "%~dp0"

REM Ejecutar backup
gobackups.exe run -c %CONFIG% --log-level %LOG_LEVEL%

if %ERRORLEVEL% == 0 (
    echo.
    echo  Backup completado con exito.
) else (
    echo.
    echo  ERROR: el backup fallo. Revisa los logs arriba.
    exit /b 1
)

endlocal
