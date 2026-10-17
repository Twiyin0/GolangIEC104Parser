// Package main — 窗口/任务栏图标：Wails v2 会用其内置默认图标覆盖窗口图标，
// 故启动后通过 Win32 WM_SETICON 把 exe 内嵌图标（rsrc.syso 生成的 logo）重新指定给窗口。
package main

import (
	"fmt"
	"os"
	"time"
	"unsafe"

	winapi "golang.org/x/sys/windows"
)

// windowClassName 与 options.App.Windows.WindowClassName 保持一致（用于查找窗口句柄）。
const windowClassName = "104ParserRealTimeWnd"

// applyWindowIcon 异步设置窗口图标（大图标=任务栏/Alt+Tab，小图标=标题栏）。
// 完成后回调一次结果（用于日志/状态栏提示）。
// 诊断开关：设置环境变量 P104_NO_ICON=1 可跳过（用于对比验证图标替换是否生效）。
func applyWindowIcon(onDone func(string)) {
	if os.Getenv("P104_NO_ICON") == "1" {
		onDone("窗口图标：已按环境变量跳过")
		return
	}
	go func() {
		user32 := winapi.NewLazySystemDLL("user32.dll")
		shell32 := winapi.NewLazySystemDLL("shell32.dll")
		sendMessage := user32.NewProc("SendMessageW")
		extractIconEx := shell32.NewProc("ExtractIconExW")

		exePath, err := os.Executable()
		if err != nil || exePath == "" {
			onDone("窗口图标：无法获取程序路径")
			return
		}
		exePtr, err := winapi.UTF16PtrFromString(exePath)
		if err != nil {
			onDone("窗口图标：路径转换失败")
			return
		}

		// 从 exe 中提取第一个图标组（rsrc.syso 生成的 logo，含多尺寸）
		var large, small winapi.Handle
		n, _, _ := extractIconEx.Call(
			uintptr(unsafe.Pointer(exePtr)), 0,
			uintptr(unsafe.Pointer(&large)),
			uintptr(unsafe.Pointer(&small)), 1)
		if n == 0 || (large == 0 && small == 0) {
			onDone("窗口图标：exe 内未找到可用图标")
			return
		}

		// 查找本进程的所有顶层可见窗口并设置图标（不依赖窗口类名）
		enumWindows := user32.NewProc("EnumWindows")
		getWindowPID := user32.NewProc("GetWindowThreadProcessId")
		isWindowVisible := user32.NewProc("IsWindowVisible")
		pid := uint32(os.Getpid())
		var targets []winapi.Handle
		cb := winapi.NewCallback(func(h uintptr, _ uintptr) uintptr {
			var wpid uint32
			getWindowPID.Call(h, uintptr(unsafe.Pointer(&wpid)))
			if wpid != pid {
				return 1 // 继续枚举
			}
			if vis, _, _ := isWindowVisible.Call(h); vis == 0 {
				return 1
			}
			targets = append(targets, winapi.Handle(h))
			return 1
		})

		const (
			wmSetIcon = 0x0080
			iconSmall = 0
			iconBig   = 1
		)
		for i := 0; i < 25; i++ { // 窗口创建需要一点时间，轮询查找
			targets = targets[:0]
			enumWindows.Call(cb, 0)
			if len(targets) > 0 {
				for _, h := range targets {
					if large != 0 {
						sendMessage.Call(uintptr(h), wmSetIcon, iconBig, uintptr(large))
					}
					if small != 0 {
						sendMessage.Call(uintptr(h), wmSetIcon, iconSmall, uintptr(small))
					}
				}
				onDone(fmt.Sprintf("窗口图标已应用（logo，窗口数 %d）", len(targets)))
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		onDone("窗口图标：未找到窗口句柄")
	}()
}
