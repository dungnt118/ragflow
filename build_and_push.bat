@echo off
chcp 65001 >nul
title RagFlow - Build and Push Pipeline

call "%~dp0build_and_deploy.bat"
exit /b %ERRORLEVEL%
