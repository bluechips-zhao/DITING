@echo off
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion
cd /d "%~dp0"

rem ============================================
rem  谛听 DITING 一键构建（Windows）
rem  前端静态导出 -> 内嵌 -> Go 单二进制 diting.exe
rem ============================================

rem 使用 PATH 中的 Go；工具链不依赖开发者机器路径。
set "GOROOT="
go version >nul 2>nul || (echo [ERR] 找不到 go，请安装 Go 1.26.3 或更新版本 & exit /b 1)

echo [1/4] 前端依赖安装...
pushd web
call npm ci --no-audit --no-fund || (popd & echo [ERR] npm ci 失败 & exit /b 1)

echo [2/4] 前端静态导出...
call npm run build:static || (popd & echo [ERR] 前端构建失败 & exit /b 1)
popd

echo [3/4] 拷贝产物到内嵌目录...
if exist server\webui\dist rmdir /s /q server\webui\dist
robocopy web\out server\webui\dist /e /nfl /ndl /njh /njs >nul
if errorlevel 8 (echo [ERR] 拷贝失败 & exit /b 1)

echo [4/4] 编译单二进制...
set "CGO_ENABLED=0"
set "GOPROXY=https://goproxy.cn,direct"
go build -trimpath -tags embedui -ldflags "-s -w -X main.version=2.3.0" -o diting.exe ./cmd/artex || (echo [ERR] Go 编译失败 & exit /b 1)

echo.
echo ============================================
echo  构建完成: %CD%\diting.exe
echo  启动: diting.exe -addr :8787 -proxy :8788
echo ============================================
