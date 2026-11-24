//go:build darwin

package main

// Touch Bar 集成：macOS 专属，纯 Go（purego + Objective-C 运行时），不使用 cgo。
//
// Apple 没有公开 API 可以把自定义视图放进 Touch Bar 右侧的 Control Strip
// （公开的 NSTouchBar 只能显示当前前台 App 的内容）。这里用与 TouchSwitcher /
// MTMR 相同的私有接口，让网速固定在 Control Strip 中，切换 App 也不消失：
//
//   - DFRElementSetControlStripPresenceForIdentifier（DFRFoundation）
//   - +[NSTouchBarItem addSystemTrayItem:] / removeSystemTrayItem:
//
// 所有 Objective-C 消息都必须从主线程发送。调用方（WhenReady、Window.Update、
// view）都在主线程，所以实现里不再额外切线程。私有符号解析失败时静默降级，
// 不影响 Sun Ray 本身。
//
// 布局：Touch Bar 本身是 1085x30pt，Control Strip 里一个系统按钮约 60x30pt，
// 而且系统不提供查询当前可用宽度的 API。这里用 72x30pt 的视图，上下两行分别
// 显示上行/下行；每行 13pt、固定 9pt 字号，比系统自动字号更适合这种窄条。

import (
	"log"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/ui"
)

const touchBarItemID = "com.idealisan.sunray.touchbar.speed"

// CoreGraphics geometry. NSRect is 32 bytes, passed through typed objc_msgSend
// wrappers (purego's reflection path cannot carry structs reliably).
type tbPoint struct{ X, Y float64 }
type tbSize struct{ Width, Height float64 }
type tbRect struct {
	Origin tbPoint
	Size   tbSize
}

// DFRElementSetControlStripPresenceForIdentifier(NSString *identifier, BOOL present)
type setControlStripPresenceFunc func(identifier uintptr, present bool)

var (
	tbMu       sync.Mutex
	tbInitDone bool
	tbOK       bool
	tbEnabled  bool

	tbItem        objc.ID
	tbContainer   objc.ID
	tbUpLabel     objc.ID
	tbDownLabel   objc.ID
	tbIdentifier  objc.ID
	tbSetPresence setControlStripPresenceFunc

	// Typed objc_msgSend variants for struct / float arguments.
	tbInitRect func(obj objc.ID, sel objc.SEL, rect tbRect) objc.ID
	tbSetRect  func(obj objc.ID, sel objc.SEL, rect tbRect)
	tbFont     func(cls objc.ID, sel objc.SEL, size float64) objc.ID

	tbUp, tbDown string
	selCache     sync.Map // map[string]objc.SEL
)

// touchBarCheckbox is the only piece of settings UI for this feature; it
// lives in a darwin-only file so other platforms never see the checkbox.
func touchBarCheckbox(c *ui.Context, g *globalConfig) {
	ui.Checkbox(c, &g.TouchBar, "在 Touch Bar 显示实时网速")
	touchBarSetEnabled(g.TouchBar)
}

func tbSEL(name string) objc.SEL {
	if s, ok := selCache.Load(name); ok {
		return s.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func tbClass(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// tbSend calls objc_msgSend with pointer/integer arguments only.
func tbSend(obj objc.ID, selector string, args ...any) objc.ID {
	return objc.Send[objc.ID](obj, tbSEL(selector), args...)
}

// tbString makes an autoreleased NSString from a Go string (UTF-8).
func tbString(s string) objc.ID {
	const nsUTF8StringEncoding = 4
	str := tbSend(tbClass("NSString"), "alloc")
	initialized := tbSend(str, "initWithBytes:length:encoding:",
		uintptr(unsafe.Pointer(unsafe.StringData(s))), uintptr(len(s)), uintptr(nsUTF8StringEncoding))
	runtime.KeepAlive(s)
	return tbSend(initialized, "autorelease")
}

// tbLabel builds one compact text field at the given y offset in the strip.
func tbLabel(y float64) objc.ID {
	label := tbSend(tbClass("NSTextField"), "labelWithString:", uintptr(tbString("—")))
	tbSetRect(label, tbSEL("setFrame:"), tbRect{
		Origin: tbPoint{X: 3, Y: y},
		Size:   tbSize{Width: 66, Height: 13},
	})
	tbSend(label, "setBezeled:", uintptr(0))
	tbSend(label, "setDrawsBackground:", uintptr(0))
	tbSend(label, "setEditable:", uintptr(0))
	tbSend(label, "setSelectable:", uintptr(0))
	tbSend(label, "setAlignment:", uintptr(0)) // NSTextAlignmentLeft
	tbSend(label, "setFont:", uintptr(tbFont(tbClass("NSFont"), tbSEL("systemFontOfSize:"), 9)))
	tbSend(label, "setTextColor:", uintptr(tbSend(tbClass("NSColor"), "whiteColor")))
	return label
}

// tbInit resolves the private symbols once and builds the Control Strip item.
func tbInit() bool {
	if tbInitDone {
		return tbOK
	}
	tbInitDone = true

	// AppKit registers NSCustomTouchBarItem / NSTextField; mygo already loads
	// it, but do it here too so this file stands on its own.
	_, _ = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		log.Printf("[sun-ray] Touch Bar: cannot load libobjc: %v", err)
		return false
	}
	msgSend, err := purego.Dlsym(libobjc, "objc_msgSend")
	if err != nil {
		log.Printf("[sun-ray] Touch Bar: cannot find objc_msgSend: %v", err)
		return false
	}
	purego.RegisterFunc(&tbInitRect, msgSend)
	purego.RegisterFunc(&tbSetRect, msgSend)
	purego.RegisterFunc(&tbFont, msgSend)

	h, err := purego.Dlopen("/System/Library/PrivateFrameworks/DFRFoundation.framework/DFRFoundation", purego.RTLD_NOW)
	if err != nil {
		log.Printf("[sun-ray] Touch Bar: cannot load DFRFoundation: %v", err)
		return false
	}
	sym, err := purego.Dlsym(h, "DFRElementSetControlStripPresenceForIdentifier")
	if err != nil {
		log.Printf("[sun-ray] Touch Bar: private API unavailable: %v", err)
		return false
	}
	purego.RegisterFunc(&tbSetPresence, sym)

	if tbClass("NSCustomTouchBarItem") == 0 || tbClass("NSTouchBarItem") == 0 {
		log.Printf("[sun-ray] Touch Bar: NSTouchBar classes unavailable")
		return false
	}

	tbIdentifier = tbString(touchBarItemID)
	item := tbSend(tbSend(tbClass("NSCustomTouchBarItem"), "alloc"), "initWithIdentifier:", uintptr(tbIdentifier))
	if item == 0 {
		log.Printf("[sun-ray] Touch Bar: cannot create touch bar item")
		return false
	}
	// Keep the item alive even if the system drops its reference.
	tbSend(item, "retain")
	tbItem = item

	container := tbInitRect(tbSend(tbClass("NSView"), "alloc"), tbSEL("initWithFrame:"), tbRect{
		Size: tbSize{Width: 72, Height: 30},
	})
	if container == 0 {
		log.Printf("[sun-ray] Touch Bar: cannot create container view")
		return false
	}
	tbContainer = container
	// Two 13pt rows, 12pt apart (half the previous visual gap), centered in
	// the 30pt strip: rows span y=2.5..15.5 and y=14.5..27.5.
	tbUpLabel = tbLabel(14.5)
	tbDownLabel = tbLabel(2.5)
	tbSend(container, "addSubview:", uintptr(tbUpLabel))
	tbSend(container, "addSubview:", uintptr(tbDownLabel))
	tbSend(tbItem, "setView:", uintptr(container))

	tbOK = true
	return true
}

// compactSpeed turns "1.2 MB/s" into "1.2M" so it fits a narrow strip column.
func compactSpeed(s string) string {
	if s == "" {
		return "—"
	}
	s = strings.TrimSuffix(s, "/s")
	for _, unit := range []string{" GB", " MB", " KB", " B"} {
		if strings.HasSuffix(s, unit) {
			return strings.TrimSuffix(s, unit) + strings.ToUpper(strings.TrimSpace(unit)[:1])
		}
	}
	return s
}

// applySpeedsLocked updates both rows; caller holds tbMu.
func applySpeedsLocked() {
	tbSend(tbUpLabel, "setStringValue:", uintptr(tbString("↑ "+compactSpeed(tbUp))))
	tbSend(tbDownLabel, "setStringValue:", uintptr(tbString("↓ "+compactSpeed(tbDown))))
}

// touchBarSetEnabled registers or removes the Control Strip item. Must run on
// the main thread. Repeated calls with the same value are a no-op.
func touchBarSetEnabled(enabled bool) {
	if runningTests() {
		return
	}
	tbMu.Lock()
	defer tbMu.Unlock()
	if !enabled && !tbInitDone {
		return
	}
	if !tbInit() {
		return
	}
	if enabled == tbEnabled {
		return
	}
	tbEnabled = enabled
	if enabled {
		tbSend(tbClass("NSTouchBarItem"), "addSystemTrayItem:", uintptr(tbItem))
		tbSetPresence(uintptr(tbIdentifier), true)
		applySpeedsLocked()
	} else {
		tbSetPresence(uintptr(tbIdentifier), false)
		tbSend(tbClass("NSTouchBarItem"), "removeSystemTrayItem:", uintptr(tbItem))
	}
}

// touchBarSetSpeeds publishes the latest per-second rates to the Touch Bar.
// Called from Window.Update, i.e. on the main thread.
func touchBarSetSpeeds(up, down string) {
	if runningTests() {
		return
	}
	tbMu.Lock()
	defer tbMu.Unlock()
	tbUp, tbDown = up, down
	if tbEnabled && tbOK {
		applySpeedsLocked()
	}
}

// touchBarShutdown removes the item on quit so it does not linger in the
// Control Strip after the process is gone.
func touchBarShutdown() {
	if runningTests() {
		return
	}
	tbMu.Lock()
	defer tbMu.Unlock()
	if !tbEnabled || !tbOK {
		return
	}
	tbEnabled = false
	tbSetPresence(uintptr(tbIdentifier), false)
	tbSend(tbClass("NSTouchBarItem"), "removeSystemTrayItem:", uintptr(tbItem))
}
