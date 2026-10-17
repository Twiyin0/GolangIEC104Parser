@echo off
rem ============================================================
rem  104ParserRealTime 运行脚本（自动以管理员权限启动）
rem  网口旁听（Npcap 抓包）需要管理员权限；静态解析等不受影响
rem ============================================================
chcp 65001 >nul
setlocal
cd /d "%~dp0"

if not exist "build\bin\104ParserRealTime.exe" (
    echo [提示] 未找到 build\bin\104ParserRealTime.exe，请先双击 build.bat 编译
    pause
    exit /b 1
)

echo 以管理员权限启动 104ParserRealTime（若弹出 UAC 请允许）...
powershell -NoProfile -Command "Start-Process -FilePath '%~dp0build\bin\104ParserRealTime.exe' -Verb RunAs"
if errorlevel 1 (
    echo [失败] 启动被取消或权限不足
    pause
    exit /b 1
)
exit /b 0
