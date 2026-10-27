@echo off
chcp 65001 >nul
title RagFlow - Build and Deploy Pipeline

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build_and_deploy.ps1"

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo [LOI] Script gap loi trong qua trinh thuc thi! Ma loi: %ERRORLEVEL%
    pause
    exit /b %ERRORLEVEL%
)

exit /b 0
