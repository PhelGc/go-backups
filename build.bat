@echo off
echo ============================================
echo  GoBackups - Build
echo ============================================
echo.

set LDFLAGS=-ldflags "-s -w"

echo Compilando gobackups.exe...
go build %LDFLAGS% -o gobackups.exe ./cmd/gobackups/
if %errorlevel% neq 0 (
    echo ERROR: fallo al compilar gobackups.exe
    exit /b 1
)
echo gobackups.exe - OK

echo Compilando backupserver.exe...
go build %LDFLAGS% -o backupserver.exe ./cmd/backupserver/
if %errorlevel% neq 0 (
    echo ERROR: fallo al compilar backupserver.exe
    exit /b 1
)
echo backupserver.exe - OK

echo Compilando webhookserver.exe...
go build %LDFLAGS% -o webhookserver.exe ./cmd/webhookserver/
if %errorlevel% neq 0 (
    echo ERROR: fallo al compilar webhookserver.exe
    exit /b 1
)
echo webhookserver.exe - OK

echo.
echo Build completado con exito.
