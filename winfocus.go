// Package main — 宿主层键盘焦点修复。
//
// WebView2 存在“内容未获得键盘焦点前不接收键盘事件”的行为
//（WebView2Feedback #5157：Content Does Not Receive Keyboard Events Until Content Clicked）。
// 实测症状：窗口可见、鼠标可点、输入框能出现聚焦样式，但键盘敲击毫无反应
//（此时进程非前台，GetGUIThreadInfo 的 focusHwnd = 0，按键被送往前台窗口）。
//
// 修复：启动后把窗口置前（BringWindowToTop + SetForegroundWindow，跨线程用 AttachThreadInput
// 规避前台锁），并把键盘焦点交给 WebView2 渲染子窗口，随后校验焦点确实落在渲染窗口上。
package main

import (
	"strings"
	"time"
	"unsafe"

	winapi "golang.org/x/sys/windows"
)

// guiThreadInfo 对应 Win32 GUITHREADINFO（x64 布局）。
type guiThreadInfo struct {
	cbSize        uint32
	flags         uint32
	hwndActive    winapi.Handle
	hwndFocus     winapi.Handle
	hwndCapture   winapi.Handle
	hwndMenuOwner winapi.Handle
	hwndMoveSize  winapi.Handle
	hwndCaret     winapi.Handle
	rcCaret       struct{ Left, Top, Right, Bottom int32 }
}

// focusWebView 启动后把窗口置前并把键盘焦点交给 WebView2 渲染窗口（幂等、失败仅记录日志）。
func focusWebView(onDone func(string)) {
	go func() {
		user32 := winapi.NewLazySystemDLL("user32.dll")
		kernel32 := winapi.NewLazySystemDLL("kernel32.dll")
		findWindow := user32.NewProc("FindWindowW")
		enumChildWindows := user32.NewProc("EnumChildWindows")
		getClassName := user32.NewProc("GetClassNameW")
		setFocus := user32.NewProc("SetFocus")
		setForegroundWindow := user32.NewProc("SetForegroundWindow")
		bringWindowToTop := user32.NewProc("BringWindowToTop")
		getForegroundWindow := user32.NewProc("GetForegroundWindow")
		getWindowThreadProcessID := user32.NewProc("GetWindowThreadProcessId")
		attachThreadInput := user32.NewProc("AttachThreadInput")
		getGUIThreadInfo := user32.NewProc("GetGUIThreadInfo")
		getCurrentThreadID := kernel32.NewProc("GetCurrentThreadId")

		cls, err := winapi.UTF16PtrFromString(windowClassName)
		if err != nil {
			onDone("键盘焦点：窗口类名转换失败")
			return
		}
		myTid, _, _ := getCurrentThreadID.Call()
		for i := 0; i < 25; i++ { // 窗口与 WebView2 子窗口创建需要时间，轮询等待
			hwnd, _, _ := findWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
			if hwnd != 0 {
				if child := findRenderChild(enumChildWindows, getClassName, hwnd); child != 0 {
					// ① 窗口置前：非前台时键盘事件会被前台窗口吃掉
					fg, _, _ := getForegroundWindow.Call()
					fgTid := uintptr(0)
					if fg != 0 {
						var p uint32
						fgTid, _, _ = getWindowThreadProcessID.Call(fg, uintptr(unsafe.Pointer(&p)))
					}
					attachedFg := attachInput(attachThreadInput, myTid, fgTid)
					bringWindowToTop.Call(hwnd)
					setForegroundWindow.Call(hwnd)
					detachInput(attachThreadInput, myTid, fgTid, attachedFg)

					// ② 键盘焦点交给 WebView2 渲染子窗口（SetFocus 只对同线程窗口有效 → 先附加输入队列）
					var wpid uint32
					wndTid, _, _ := getWindowThreadProcessID.Call(child, uintptr(unsafe.Pointer(&wpid)))
					attached := attachInput(attachThreadInput, myTid, wndTid)
					setFocus.Call(child)
					detachInput(attachThreadInput, myTid, wndTid, attached)

					// ③ 校验：该 UI 线程的焦点窗口是否为渲染子窗口
					if focusIsRenderChild(getGUIThreadInfo, wndTid, child) {
						onDone("键盘焦点已就绪（可直接输入）")
						return
					}
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		onDone("键盘焦点：未能确认（可先点一下界面再输入）")
	}()
}


func attachInput(attachThreadInput *winapi.LazyProc, myTid, targetTid uintptr) bool {
	if targetTid == 0 || targetTid == myTid {
		return false
	}
	r, _, _ := attachThreadInput.Call(myTid, targetTid, 1)
	return r != 0
}

func detachInput(attachThreadInput *winapi.LazyProc, myTid, targetTid uintptr, attached bool) {
	if attached {
		attachThreadInput.Call(myTid, targetTid, 0)
	}
}

// focusIsRenderChild 查询目标线程的焦点窗口，判断是否落在 WebView2 渲染子窗口上。
func focusIsRenderChild(getGUIThreadInfo *winapi.LazyProc, tid uintptr, child uintptr) bool {
	var g guiThreadInfo
	g.cbSize = uint32(unsafe.Sizeof(g))
	r, _, _ := getGUIThreadInfo.Call(tid, uintptr(unsafe.Pointer(&g)))
	if r == 0 {
		return false
	}
	return uintptr(g.hwndFocus) == child
}

// findRenderChild 在顶层窗口的后代中查找 WebView2 渲染子窗口
//（Chrome_RenderWidgetHostHWND 优先，其次 Chrome_WidgetWin_*）。
func findRenderChild(enumChildWindows, getClassName *winapi.LazyProc, hwnd uintptr) uintptr {
	var fallback uintptr
	cb := winapi.NewCallback(func(h uintptr, _ uintptr) uintptr {
		var buf [256]uint16
		n, _, _ := getClassName.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return 1
		}
		name := winapi.UTF16ToString(buf[:n])
		switch {
		case strings.HasPrefix(name, "Chrome_RenderWidgetHostHWND"):
			fallback = h
			return 0 // 命中渲染窗口 → 停止枚举
		case strings.HasPrefix(name, "Chrome_WidgetWin"):
			if fallback == 0 {
				fallback = h
			}
		}
		return 1
	})
	enumChildWindows.Call(hwnd, cb, 0)
	return fallback
}
