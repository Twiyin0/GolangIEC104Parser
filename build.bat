@echo off
rem ============================================================
rem  104ParserRealTime 一键编译脚本
rem  用法：双击本文件，或命令行执行  build.bat [--nopause]
rem  产物：build\bin\104ParserRealTime.exe（单文件，直接可运行）
rem ============================================================
chcp 65001 >nul
setlocal
cd /d "%~dp0"

rem ---- Go 环境（本机安装位置）----
set "GO_DIR=E:\Developer\evn\go-1-27-2\bin"
set "PATH=%GO_DIR%;%PATH%"
set "GOPROXY=https://goproxy.cn,direct"
rem 如 go 不在上述位置，可改用系统 PATH 中已安装的 go

echo [1/6] 检查 Go 环境...
where go >nul 2>&1
if errorlevel 1 (
    echo   [错误] 未找到 go 命令（%GO_DIR%）
    goto :err
)
for /f "tokens=*" %%v in ('go version') do echo   %%v

echo [2/6] 拉取/校验依赖（go mod tidy，首次需联网）...
go mod tidy
if errorlevel 1 goto :err

echo [3/6] 单元测试（协议解析 + 导出导入闭环 + 点表）...
go test ./internal/...
if errorlevel 1 (
    echo   [警告] 单元测试未通过，仍继续编译——建议先排查测试失败原因
) else (
    echo   全部测试通过
)

echo [4/6] 生成程序图标（logo.png 到 icon.ico 到 rsrc.syso，失败则使用默认图标）...
if not exist "build\windows" mkdir "build\windows"
go run ./scripts/mkico -in "logo.png" -out "build\windows\icon.ico"
if errorlevel 1 (
    echo   [警告] 图标生成失败，跳过（exe 使用默认图标）
) else (
    go run github.com/akavel/rsrc@latest -ico "build\windows\icon.ico" -o "rsrc.syso" -arch amd64
    if errorlevel 1 (
        echo   [警告] rsrc 生成失败，跳过（exe 使用默认图标）
    ) else (
        echo   图标资源已生成: rsrc.syso
    )
)

echo [5/6] 编译单文件 exe（Wails 需 -tags desktop,production；前端资源内嵌，windowsgui）...
rem 若程序正在运行会锁定 exe，先结束旧实例（仅本程序）
taskkill /IM 104ParserRealTime.exe /F >nul 2>&1
if not exist "build\bin" mkdir "build\bin"
go build -trimpath -tags desktop,production -ldflags "-s -w -H windowsgui" -o "build\bin\104ParserRealTime.exe" .
if errorlevel 1 goto :err

echo [6/6] 验证产物...
if not exist "build\bin\104ParserRealTime.exe" goto :err
for %%f in ("build\bin\104ParserRealTime.exe") do echo   产物: %%~ff  （%%~zf 字节）

echo.
echo ============================================================
echo   编译成功: %~dp0build\bin\104ParserRealTime.exe
echo   运行提示: 网口旁听需管理员权限（右键"以管理员身份运行"）
echo             或直接双击 run.bat（自动申请管理员权限）
echo   静态解析 / 导入导出 / 串口 等功能无需管理员权限
echo ============================================================
if /i "%~1"=="--nopause" goto :eof
pause
exit /b 0

:err
echo.
echo   [失败] 编译未完成，请查看上方错误信息
if /i "%~1"=="--nopause" exit /b 1
pause
exit /b 1
