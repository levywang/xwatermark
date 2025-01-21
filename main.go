package main

import (
	"fmt"
	"image"
	"log"
	"math"
	"os/user"
	"strings"
	"syscall"
	"unsafe"

	"runtime"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font/gofont/goregular"
)

var (
	// Windows API DLL 和函数
	user32                         = syscall.NewLazyDLL("user32.dll")             // user32.dll 动态链接库
	procCreateWindowEx             = user32.NewProc("CreateWindowExW")            // 创建窗口
	procDefWindowProc              = user32.NewProc("DefWindowProcW")             // 默认窗口过程
	procDestroyWindow              = user32.NewProc("DestroyWindow")              // 销毁窗口
	procDispatchMessage            = user32.NewProc("DispatchMessageW")           // 分发消息
	procGetMessage                 = user32.NewProc("GetMessageW")                // 获取消息
	procLoadCursor                 = user32.NewProc("LoadCursorW")                // 加载光标
	procRegisterClassEx            = user32.NewProc("RegisterClassExW")           // 注册窗口类
	procShowWindow                 = user32.NewProc("ShowWindow")                 // 显示窗口
	procUpdateWindow               = user32.NewProc("UpdateWindow")               // 更新窗口
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")           // 获取系统指标
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes") // 设置分层窗口属性

	// GDI+ 相关函数
	gdi32                      = syscall.NewLazyDLL("gdi32.dll")         // gdi32.dll 动态链接库
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")     // 创建兼容设备上下文
	procDeleteDC               = gdi32.NewProc("DeleteDC")               // 删除设备上下文
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap") // 创建兼容位图
	procSelectObject           = gdi32.NewProc("SelectObject")           // 选择对象
	procDeleteObject           = gdi32.NewProc("DeleteObject")           // 删除对象
	procBitBlt                 = gdi32.NewProc("BitBlt")                 // 位图传输
	procSetPixel               = gdi32.NewProc("SetPixelV")              // 设置像素

	// 屏幕尺寸
	screenWidth  uintptr // 屏幕宽度
	screenHeight uintptr // 屏幕高度

	// 水印配置
	watermarkConfig = struct {
		angle         float64 // 文字倾斜角度（负数表示向右倾斜）
		imageRotation float64 // 整体图片旋转角度（0-360度）
		fontSize      float64 // 字体大小（单位：磅）
		spaceCount    int     // 文字与时间戳之间的空格数
		color         uint32  // 水印颜色 (BGR格式：0x00BBGGRR)
		alpha         uint32  // 水印透明度 (0-255，0表示完全透明)
		spacingX      int     // 水印水平间距（像素）
		spacingY      int     // 水印垂直间距（像素）
		fontData      []byte  // 字体数据
	}{
		angle:         0.0,   // 文字倾斜角度
		imageRotation: 320.0, // 整体旋转270度
		fontSize:      20.0,
		spaceCount:    5, // 减少空格数，保持在一行
		color:         0x00ffffff,
		alpha:         7,
		spacingX:      250,           // 减小水平间距
		spacingY:      125,           // 减小垂直间距
		fontData:      goregular.TTF, // 默认使用 Go 内置字体
	}

	// 颜色转换函数：将十六进制颜色字符串转换为BGR格式
	colorHexToBGR = func(hex string) uint32 {
		// 将 "ff0000" 格式转换为 0x000000ff
		var color uint32
		if len(hex) == 6 {
			r := hex[0:2]
			g := hex[2:4]
			b := hex[4:6]
			fmt.Sscanf(r+g+b, "%x", &color)
			// 转换为 BGR 格式
			return ((color & 0xff0000) >> 16) | (color & 0x00ff00) | ((color & 0x0000ff) << 16)
		}
		return 0
	}
)

const (
	WS_EX_LAYERED            = 0x00080000
	WS_EX_TOPMOST            = 0x00000008
	LWA_ALPHA                = 0x00000002
	SM_CXSCREEN              = 0
	SM_CYSCREEN              = 1
	WS_POPUP                 = 0x80000000
	WS_EX_TRANSPARENT        = 0x00000020 // 点击穿透
	WS_EX_TOOLWINDOW         = 0x00000080 // 不在任务栏显示
	WM_NCHITTEST             = 0x0084
	HTTRANSPARENT            = ^uintptr(0) // 使用 uintptr 的最大值
	WM_PAINT                 = 0x000F
	SRCCOPY                  = 0x00CC0020
	MONITOR_DEFAULTTOPRIMARY = 0x00000001
	SM_CMONITORS             = 80 // 获取显示器数量
)

// 添加 Windows API 相关的结构体和常量定义
type (
	HANDLE    uintptr
	HWND      HANDLE
	HINSTANCE HANDLE
	HICON     HANDLE
	HCURSOR   HANDLE
	HBRUSH    HANDLE
	HMENU     HANDLE
	LPVOID    uintptr
)

type WNDCLASSEX struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   HINSTANCE
	Icon       HICON
	Cursor     HCURSOR
	Background HBRUSH
	MenuName   *uint16
	ClassName  *uint16
	IconSm     HICON
}

type POINT struct {
	X, Y int32
}

type MSG struct {
	Hwnd    HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// 添加新的常量
const (
	WM_DESTROY = 0x0002
)

// 添加 PostQuitMessage 函数
var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procPostQuitMessage = kernel32.NewProc("PostQuitMessage")
)

// 添加显示器信息结构体
type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	Flags     uint32
}

// 添加一个辅助函数来提取纯用户名
func extractUsername(fullUsername string) string {
	// 处理 domain\username 格式
	if i := strings.LastIndex(fullUsername, "\\"); i >= 0 {
		return fullUsername[i+1:]
	}
	// 处理 username@domain 格式
	if i := strings.Index(fullUsername, "@"); i >= 0 {
		return fullUsername[:i]
	}
	return fullUsername
}

func createWatermarkImage(username string) *image.RGBA {
	// 使用单个屏幕尺寸
	width := int(screenWidth)
	height := int(screenHeight)

	// 减小画布尺寸，使用1.5倍而不是2倍
	canvasSize := int(math.Sqrt(float64(width*width+height*height))) * 5 / 4
	img := image.NewRGBA(image.Rect(0, 0, canvasSize, canvasSize))

	// 使用更小的缓冲区绘制
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.Set(x, y, image.Transparent)
		}
	}

	// 加载字体
	font, err := truetype.Parse(watermarkConfig.fontData)
	if err != nil {
		log.Fatal(err)
	}

	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(font)
	c.SetFontSize(watermarkConfig.fontSize)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(image.Black)

	// 获取水印文字
	spaces := strings.Repeat(" ", watermarkConfig.spaceCount)
	// 使用过滤后的用户名
	watermarkText := fmt.Sprintf("CompanyName %s%s", extractUsername(username), spaces)

	// 计算水印间距
	spacingX := watermarkConfig.spacingX * 4 / 3
	spacingY := watermarkConfig.spacingY * 4 / 3

	// 使用倾斜角度
	angle := watermarkConfig.angle * math.Pi / 180.0

	// 优化绘制范围
	startX := -canvasSize / 2
	endX := canvasSize * 3 / 2
	startY := -canvasSize / 2
	endY := canvasSize * 3 / 2

	// 批量绘制水印
	for y := startY; y < endY; y += spacingY {
		for x := startX; x < endX; x += spacingX {
			rotX := float64(x)*math.Cos(angle) - float64(y)*math.Sin(angle)
			rotY := float64(x)*math.Sin(angle) + float64(y)*math.Cos(angle)
			rotX += float64(canvasSize) / 2
			rotY += float64(canvasSize) / 2

			if rotX >= 0 && rotX < float64(canvasSize) && rotY >= 0 && rotY < float64(canvasSize) {
				pt := freetype.Pt(int(rotX), int(rotY))
				c.DrawString(watermarkText, pt)
			}
		}
	}

	// 优化旋转和裁剪
	final := rotateAndCrop(img, watermarkConfig.imageRotation, width, height)
	runtime.GC() // 手动触发垃圾回收
	return final
}

// 优化的旋转和裁剪函数
func rotateAndCrop(img *image.RGBA, angle float64, targetWidth, targetHeight int) *image.RGBA {
	rad := angle * math.Pi / 180.0
	bounds := img.Bounds()
	w, h := float64(bounds.Dx()), float64(bounds.Dy())

	// 直接创建目标大小的图像
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	// 计算中心点
	cx, cy := w/2, h/2
	newCX, newCY := float64(targetWidth)/2, float64(targetHeight)/2

	// 只处理目标区域的像素
	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			// 反向计算源图像坐标
			dx := float64(x) - newCX
			dy := float64(y) - newCY

			oldX := dx*math.Cos(-rad) - dy*math.Sin(-rad) + cx
			oldY := dx*math.Sin(-rad) + dy*math.Cos(-rad) + cy

			if oldX >= 0 && oldX < w && oldY >= 0 && oldY < h {
				dst.Set(x, y, img.At(int(oldX), int(oldY)))
			}
		}
	}

	return dst
}

func main() {
	// 获取当前用户
	currentUser, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	// 获取屏幕尺寸
	screenWidth, _, _ = procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	screenHeight, _, _ = procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))

	// 创建水印图片（声明为全局变量）
	var watermarkImage *image.RGBA
	watermarkImage = createWatermarkImage(currentUser.Username)

	// 创建一个闭包函数来访问 watermarkImage
	wndProc := func(hwnd syscall.Handle, msg uint32, wparam, lparam uintptr) uintptr {
		switch msg {
		case WM_DESTROY:
			procPostQuitMessage.Call(0)
			return 0
		case WM_NCHITTEST:
			return HTTRANSPARENT
		case WM_PAINT:
			var ps PAINTSTRUCT
			hdc, _, _ := user32.NewProc("BeginPaint").Call(
				uintptr(hwnd),
				uintptr(unsafe.Pointer(&ps)),
			)
			if hdc == 0 {
				return 0
			}
			defer user32.NewProc("EndPaint").Call(
				uintptr(hwnd),
				uintptr(unsafe.Pointer(&ps)),
			)

			// 创建内存 DC
			memDC, _, _ := procCreateCompatibleDC.Call(hdc)
			if memDC == 0 {
				return 0
			}
			defer procDeleteDC.Call(memDC)

			// 创建位图
			bitmap, _, _ := procCreateCompatibleBitmap.Call(
				hdc,
				screenWidth,
				screenHeight,
			)
			if bitmap == 0 {
				return 0
			}
			defer procDeleteObject.Call(bitmap)

			// 选择位图到内存 DC
			oldBitmap, _, _ := procSelectObject.Call(memDC, bitmap)
			defer procSelectObject.Call(memDC, oldBitmap)

			// 将图片数据复制到位图，使用配置的颜色
			for y := 0; y < watermarkImage.Bounds().Dy(); y++ {
				for x := 0; x < watermarkImage.Bounds().Dx(); x++ {
					_, _, _, a := watermarkImage.At(x, y).RGBA()
					if a > 0 {
						procSetPixel.Call(
							memDC,
							uintptr(x),
							uintptr(y),
							uintptr(watermarkConfig.color),
						)
					}
				}
			}

			// 将位图复制到窗口
			procBitBlt.Call(
				hdc,
				0, 0,
				screenWidth,
				screenHeight,
				memDC,
				0, 0,
				SRCCOPY,
			)

			return 0
		default:
			ret, _, _ := procDefWindowProc.Call(
				uintptr(hwnd),
				uintptr(msg),
				wparam,
				lparam,
			)
			return ret
		}
	}

	// 创建透明窗口
	className := syscall.StringToUTF16Ptr("WatermarkClass")
	windowName := syscall.StringToUTF16Ptr("Watermark")

	wndClass := WNDCLASSEX{
		CbSize:     uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      0,
		WndProc:    syscall.NewCallback(wndProc),
		ClsExtra:   0,
		WndExtra:   0,
		Instance:   0,
		Icon:       0,
		Cursor:     0,
		Background: 0,
		MenuName:   nil,
		ClassName:  className,
		IconSm:     0,
	}

	// 修改错误处理
	ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wndClass)))
	if ret == 0 {
		log.Fatal(err)
	}

	hwnd, _, _ := procCreateWindowEx.Call(
		WS_EX_LAYERED|WS_EX_TOPMOST|WS_EX_TRANSPARENT|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_POPUP,
		0, 0,
		uintptr(screenWidth),  // 使用总宽度
		uintptr(screenHeight), // 使用最大高度
		0, 0, 0, 0,
	)

	// 修改窗口透明度设置
	procSetLayeredWindowAttributes.Call(
		hwnd,
		0,
		uintptr(watermarkConfig.alpha),
		LWA_ALPHA,
	)

	procShowWindow.Call(hwnd, 1)
	procUpdateWindow.Call(hwnd)

	// 消息循环
	var msg MSG
	for {
		ret, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 {
			break
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

type PAINTSTRUCT struct {
	Hdc         HDC
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

type HDC HANDLE
