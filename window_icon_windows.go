//go:build windows

package main

import (
	_ "embed"
	"log/slog"
	"os"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// windowsIcon is the multi resolution .ico the icon pipeline generates; it is embedded
// so the window icon does not depend on files shipped next to the executable.
//
//go:embed build/windows/icon.ico
var windowsIcon []byte

// Wails registers its window class with the generic Windows application icon, so the
// exe resource alone is not enough: the window (and therefore the taskbar button) gets
// its icon through WM_SETICON once the native window exists.
const (
	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
	imageIcon      = 1
	lrLoadFromFile = 0x0010
	lrDefaultSize  = 0x0040
	smCXSmIcon     = 49
	smCYSmIcon     = 50
	// DWMWA_USE_IMMERSIVE_DARK_MODE (Windows 10 1809+: 20, 1803: 19).
	dwmwaUseImmersiveDarkMode = 20
)

var (
	user32DLL            = windows.NewLazySystemDLL("user32.dll")
	dwmapiDLL            = windows.NewLazySystemDLL("dwmapi.dll")
	procLoadImage        = user32DLL.NewProc("LoadImageW")
	procSendMessage      = user32DLL.NewProc("SendMessageW")
	procGetSystemMetrics = user32DLL.NewProc("GetSystemMetrics")
	procDwmSetAttribute  = dwmapiDLL.NewProc("DwmSetWindowAttribute")
)

// setWindowIcon waits for the native window and then assigns the application icon.
func setWindowIcon(window *application.WebviewWindow) {
	go func() {
		for attempt := 0; attempt < 400; attempt++ {
			hwnd := window.NativeWindow()
			if hwnd == nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			iconPath, cleanup, err := writeIconFile()
			if err != nil {
				slog.Warn("cannot prepare the window icon", "error", err)
				return
			}
			defer cleanup()
			big := loadIconFromFile(iconPath, 0, 0, lrLoadFromFile|lrDefaultSize)
			cx, _, _ := procGetSystemMetrics.Call(smCXSmIcon)
			cy, _, _ := procGetSystemMetrics.Call(smCYSmIcon)
			small := loadIconFromFile(iconPath, int(cx), int(cy), lrLoadFromFile)
			handle := uintptr(hwnd)
			if big != 0 {
				procSendMessage.Call(handle, wmSetIcon, iconBig, big)
			}
			if small != 0 {
				procSendMessage.Call(handle, wmSetIcon, iconSmall, small)
			}
			if big == 0 || small == 0 {
				slog.Warn("the window icon could not be loaded", "big", big, "small", small)
			}
			return
		}
	}()
}

// writeIconFile stores the embedded icon in a temporary file, which is what LoadImage
// needs; the file can be removed as soon as the icon has been loaded.
func writeIconFile() (string, func(), error) {
	file, err := os.CreateTemp("", "gokeep-icon-*.ico")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	if _, err := file.Write(windowsIcon); err != nil {
		file.Close()
		os.Remove(path)
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", func() {}, err
	}
	return path, func() { os.Remove(path) }, nil
}

func loadIconFromFile(path string, cx, cy int, flags uintptr) uintptr {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	handle, _, _ := procLoadImage.Call(
		0,
		uintptr(unsafe.Pointer(pointer)),
		imageIcon,
		uintptr(cx),
		uintptr(cy),
		flags,
	)
	return handle
}

// setWindowDarkMode switches the native title bar between the light and the dark
// variant, so the top of the window follows the theme chosen inside the application
// instead of the Windows setting.
func setWindowDarkMode(window *application.WebviewWindow, dark bool) {
	go func() {
		for attempt := 0; attempt < 400; attempt++ {
			hwnd := window.NativeWindow()
			if hwnd == nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			value := int32(0)
			if dark {
				value = 1
			}
			result, _, _ := procDwmSetAttribute.Call(
				uintptr(hwnd),
				dwmwaUseImmersiveDarkMode,
				uintptr(unsafe.Pointer(&value)),
				unsafe.Sizeof(value),
			)
			if result != 0 {
				// Older Windows builds use attribute 19.
				procDwmSetAttribute.Call(
					uintptr(hwnd),
					dwmwaUseImmersiveDarkMode-1,
					uintptr(unsafe.Pointer(&value)),
					unsafe.Sizeof(value),
				)
			}
			return
		}
	}()
}
