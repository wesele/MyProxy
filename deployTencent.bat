@echo off
chcp 65001 >nul
python scripts/deployTencent.py %*
if errorlevel 1 (
    echo.
    echo Deploy failed, check logs above
    pause
)
