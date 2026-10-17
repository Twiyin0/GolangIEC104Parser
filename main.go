package main

import (
	"embed"
	"log"
	"os"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wopts "github.com/wailsapp/wails/v2/pkg/options/windows"
	winapi "golang.org/x/sys/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	err := runApp(app)
	if err != nil {
		// 常见于上一实例刚退出、WebView2 用户数据目录尚未释放 → 稍候自动重试一次
		log.Println("首次启动失败，1.5s 后重试:", err)
		time.Sleep(1500 * time.Millisecond)
		err = runApp(app)
	}
	if err != nil {
		// windowsgui 子系统无控制台，启动失败必须弹窗提示（否则表现为"双击无反应"）
		log.Println("启动失败:", err)
		errorBox("程序启动失败：\n\n" + err.Error() +
			"\n\n常见原因：\n1) 缺少 WebView2 Runtime（Win10/11 一般自带，可从微软官网安装）\n" +
			"2) 本程序上一实例尚未完全退出（请稍候几秒后重试）\n" +
			"3) 用户数据目录被占用（重启本程序或稍后重试即可）\n\n" +
			"注意：请勿结束系统中其他的 msedgewebview2.exe 进程——它们为其它软件共用。")
		os.Exit(1)
	}
}

// runApp 启动 Wails 应用（可重试）。
func runApp(app *App) error {
	return wails.Run(&options.App{
		Title:     "104ParserRealTime — 101/104 实时报文解析",
		Width:     1440,
		Height:    920,
		MinWidth:  1120,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
		Windows: &wopts.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			WindowClassName:      windowClassName, // 供 applyWindowIcon 查找窗口句柄
		},
	})
}

// errorBox 弹出 Windows 错误对话框（不依赖控制台）。
func errorBox(msg string) {
	user32 := winapi.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	title, _ := winapi.UTF16PtrFromString("104ParserRealTime")
	text, _ := winapi.UTF16PtrFromString(msg)
	const mbIconError = 0x10
	proc.Call(0,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		uintptr(mbIconError))
}

