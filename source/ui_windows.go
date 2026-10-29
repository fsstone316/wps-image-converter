//go:build windows && !cli

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Windows native UI, no WPS / Office / Python required. Converter core is unchanged from v2.7.
const (
	version                     = "2.9"
	wmCreate                    = 0x0001
	wmDestroy                   = 0x0002
	wmSize                      = 0x0005
	wmPaint                     = 0x000F
	wmClose                     = 0x0010
	wmCommand                   = 0x0111
	wmNotify                    = 0x004E
	wmDropFiles                 = 0x0233
	wmApp                       = 0x8000
	wmSetFont                   = 0x0030
	wsOverlappedWindow          = 0x00CF0000
	wsChild                     = 0x40000000
	wsVisible                   = 0x10000000
	wsTabstop                   = 0x00010000
	wsVscroll                   = 0x00200000
	wsBorder                    = 0x00800000
	wsClipChildren              = 0x02000000
	wsExClientEdge              = 0x00000200
	bsPushButton                = 0x0
	bsAutoCheckBox              = 0x3
	bmGetCheck                  = 0x00F0
	cmdPick                     = 1101
	cmdOpen                     = 1102
	cmdFolder                   = 1103
	cmdClear                    = 1104
	cmdEnlarge                  = 1105
	lvsReport                   = 0x0001
	lvsSingleSel                = 0x0004
	lvsShowSelAlways            = 0x0008
	lvsExFullRowSelect          = 0x00000020
	lvsExDoubleBuffer           = 0x00010000
	lvmFirst                    = 0x1000
	lvmInsertColumnW            = lvmFirst + 97
	lvmInsertItemW              = lvmFirst + 77
	lvmSetItemTextW             = lvmFirst + 116
	lvmGetNextItem              = lvmFirst + 12
	lvmSetExtendedListViewStyle = lvmFirst + 54
	lvmDeleteAllItems           = lvmFirst + 9
	lvmSetColumnWidth           = lvmFirst + 30
	lvifText                    = 0x0001
	lvniSelected                = 0x0002
	lvcfFmt                     = 0x0001
	lvcfWidth                   = 0x0002
	lvcfText                    = 0x0004
	iccListViewClasses          = 0x00000001
	nmDblClick                  = -3
	lvnItemChanged              = -101
	swShow                      = 5
	colorTransparent            = 1
	maxHistory                  = 500
	wmContextMenu               = 0x007B
	lvmHitTest                  = lvmFirst + 18
	lvmSetItemState             = lvmFirst + 43
	wmGetTextLength             = 0x000E
	bmSetCheck                  = 0x00F1
	cbAddString                 = 0x0143
	cbGetCurSel                 = 0x0147
	cbSetCurSel                 = 0x014E
	cmdPickFolder               = 1110
	cmdStart                    = 1111
	cmdUniform                  = 1112
	cmdWidth                    = 1113
	cmdAbout                    = 1114
	cmdHelp                     = 1115
	menuOpenResult              = 2101
	menuOpenSource              = 2102
	menuReveal                  = 2103
	menuRename                  = 2104
	menuDeleteResult            = 2105
	menuRemove                  = 2106
	menuRequeue                 = 2107
)

var (
	u32                   = syscall.NewLazyDLL("user32.dll")
	k32                   = syscall.NewLazyDLL("kernel32.dll")
	cd32                  = syscall.NewLazyDLL("comdlg32.dll")
	sh32                  = syscall.NewLazyDLL("shell32.dll")
	gd32                  = syscall.NewLazyDLL("gdi32.dll")
	cc32                  = syscall.NewLazyDLL("comctl32.dll")
	ux32                  = syscall.NewLazyDLL("uxtheme.dll")
	pRegisterClassEx      = u32.NewProc("RegisterClassExW")
	pCreateWindowEx       = u32.NewProc("CreateWindowExW")
	pDefWindowProc        = u32.NewProc("DefWindowProcW")
	pShowWindow           = u32.NewProc("ShowWindow")
	pUpdateWindow         = u32.NewProc("UpdateWindow")
	pGetMessage           = u32.NewProc("GetMessageW")
	pTranslateMessage     = u32.NewProc("TranslateMessage")
	pDispatchMessage      = u32.NewProc("DispatchMessageW")
	pPostQuitMessage      = u32.NewProc("PostQuitMessage")
	pPostMessage          = u32.NewProc("PostMessageW")
	pSendMessage          = u32.NewProc("SendMessageW")
	pMessageBox           = u32.NewProc("MessageBoxW")
	pSetWindowText        = u32.NewProc("SetWindowTextW")
	pEnableWindow         = u32.NewProc("EnableWindow")
	pLoadCursor           = u32.NewProc("LoadCursorW")
	pLoadImage            = u32.NewProc("LoadImageW")
	pBeginPaint           = u32.NewProc("BeginPaint")
	pEndPaint             = u32.NewProc("EndPaint")
	pGetClientRect        = u32.NewProc("GetClientRect")
	pMoveWindow           = u32.NewProc("MoveWindow")
	pInvalidateRect       = u32.NewProc("InvalidateRect")
	pDrawIconEx           = u32.NewProc("DrawIconEx")
	pGetModuleHandle      = k32.NewProc("GetModuleHandleW")
	pGetOpenFileName      = cd32.NewProc("GetOpenFileNameW")
	pGetSaveFileName      = cd32.NewProc("GetSaveFileNameW")
	pCommDlgExtendedError = cd32.NewProc("CommDlgExtendedError")
	pDragAcceptFiles      = sh32.NewProc("DragAcceptFiles")
	pDragQueryFile        = sh32.NewProc("DragQueryFileW")
	pDragFinish           = sh32.NewProc("DragFinish")
	pSHBrowseForFolder    = sh32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDList  = sh32.NewProc("SHGetPathFromIDListW")
	pCoTaskMemFree        = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree")
	pCoInitializeEx       = syscall.NewLazyDLL("ole32.dll").NewProc("CoInitializeEx")
	pCoUninitialize       = syscall.NewLazyDLL("ole32.dll").NewProc("CoUninitialize")
	pCreatePopupMenu      = u32.NewProc("CreatePopupMenu")
	pAppendMenu           = u32.NewProc("AppendMenuW")
	pTrackPopupMenu       = u32.NewProc("TrackPopupMenu")
	pDestroyMenu          = u32.NewProc("DestroyMenu")
	pGetCursorPos         = u32.NewProc("GetCursorPos")
	pScreenToClient       = u32.NewProc("ScreenToClient")
	pSetWindowLongPtr     = u32.NewProc("SetWindowLongPtrW")
	pCallWindowProc       = u32.NewProc("CallWindowProcW")
	pSetFocus             = u32.NewProc("SetFocus")
	pShellExecute         = sh32.NewProc("ShellExecuteW")
	pInitCommonControlsEx = cc32.NewProc("InitCommonControlsEx")
	pSetWindowTheme       = ux32.NewProc("SetWindowTheme")
	pCreateFont           = gd32.NewProc("CreateFontW")
	pSelectObject         = gd32.NewProc("SelectObject")
	pCreateSolidBrush     = gd32.NewProc("CreateSolidBrush")
	pDeleteObject         = gd32.NewProc("DeleteObject")
	pFillRect             = u32.NewProc("FillRect")
	pSetBkMode            = gd32.NewProc("SetBkMode")
	pSetTextColor         = gd32.NewProc("SetTextColor")
	pTextOut              = gd32.NewProc("TextOutW")
)

type wndClassEx struct {
	CbSize, Style                            uint32
	LpfnWndProc                              uintptr
	CbClsExtra, CbWndExtra                   int32
	HInstance, HIcon, HCursor, HbrBackground uintptr
	LpszMenuName, LpszClassName              *uint16
	HIconSm                                  uintptr
}
type winMsg struct {
	Hwnd           uintptr
	Message, Pad   uint32
	WParam, LParam uintptr
	Time           uint32
	PtX, PtY       int32
	LPrivate       uint32
}
type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	Hdc                uintptr
	Erase              int32
	R                  rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type openFileName struct {
	LStructSize                                     uint32
	HwndOwner, HInstance                            uintptr
	LpstrFilter, LpstrCustomFilter                  *uint16
	NMaxCustFilter, NFilterIndex                    uint32
	LpstrFile                                       *uint16
	NMaxFile                                        uint32
	LpstrFileTitle                                  *uint16
	NMaxFileTitle                                   uint32
	LpstrInitialDir, LpstrTitle                     *uint16
	Flags                                           uint32
	NFileOffset, NFileExtension                     uint16
	LpstrDefExt                                     *uint16
	LCustData, LpfnHook, LpTemplateName, PvReserved uintptr
	DwReserved, FlagsEx                             uint32
}
type initCommonControlsEx struct{ Size, ICC uint32 }
type point struct{ X, Y int32 }
type browseInfo struct {
	HwndOwner, PidlRoot       uintptr
	PszDisplayName, LpszTitle *uint16
	UlFlags                   uint32
	Lpfn, LParam              uintptr
	IImage                    int32
}
type lvHitTest struct {
	Pt                      point
	Flags                   uint32
	IItem, ISubItem, IGroup int32
}
type lvColumn struct {
	Mask         uint32
	Fmt          int32
	Cx           int32
	Text         *uint16
	MaxText      int32
	SubItem      int32
	Image        int32
	Order        int32
	MinWidth     int32
	DefaultWidth int32
	IdealWidth   int32
}
type lvItem struct {
	Mask             uint32
	IItem, ISubItem  int32
	State, StateMask uint32
	Text             *uint16
	MaxText          int32
	Image            int32
	LParam           uintptr
	Indent           int32
	GroupID          int32
	Columns          uint32
	PColumns         uintptr
	ColFmt           uintptr
	Group            int32
}
type nmHdr struct {
	HwndFrom, IDFrom uintptr
	Code             uint32
	Pad              uint32
}

type record struct {
	Input  string `json:"input"`
	Output string `json:"output"`
	Status string `json:"status"`
	Images int    `json:"images"`
	Detail string `json:"detail"`
	At     string `json:"at"`
}
type event struct {
	index                  int
	status, output, detail string
	images                 int
	done                   bool
}
type appState struct {
	hwnd, list, pick, pickFolder, start, clear, grow, uniform, widthCombo, about, help uintptr
	font, fontSmall, fontTitle, fontHeading, icon                                      uintptr
	bgBrush, headerBrush                                                               uintptr
	records                                                                            []record
	running                                                                            bool
	width, height                                                                      int32
	status                                                                             string
	success, failure                                                                   int
}

var app appState
var events = make(chan event, 512)
var originalListWndProc uintptr
var listWndCallback uintptr

// Let files dropped over the large list surface reach the same import queue
// as files dropped over the surrounding main window.
func listWindowProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	if msg == wmDropFiles {
		addToQueue(draggedFiles(w))
		return 0
	}
	if originalListWndProc != 0 {
		ret, _, _ := pCallWindowProc.Call(originalListWndProc, hwnd, uintptr(msg), w, l)
		return ret
	}
	ret, _, _ := pDefWindowProc.Call(hwnd, uintptr(msg), w, l)
	return ret
}

func utf(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func callSend(h uintptr, msg uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessage.Call(h, uintptr(msg), w, l)
	return r
}
func setText(h uintptr, s string) {
	p := utf(s)
	pSetWindowText.Call(h, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}
func message(s string) {
	a, b := utf(s), utf("WPS 图片转 Excel · "+version)
	pMessageBox.Call(app.hwnd, uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x10)
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
}
func color(r, g, b byte) uintptr { return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16) }
func gdiFont(height int32, weight int32) uintptr {
	family := utf("Microsoft YaHei UI")
	r, _, _ := pCreateFont.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(family)))
	runtime.KeepAlive(family)
	return r
}
func control(class, title string, style uint32, ex uint32, x, y, w, h int32, id uintptr) uintptr {
	c, t := utf(class), utf(title)
	inst, _, _ := pGetModuleHandle.Call(0)
	hwnd, _, _ := pCreateWindowEx.Call(uintptr(ex), uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), app.hwnd, id, inst, 0)
	runtime.KeepAlive(c)
	runtime.KeepAlive(t)
	if hwnd != 0 && app.font != 0 {
		callSend(hwnd, wmSetFont, app.font, 1)
	}
	return hwnd
}
func drawText(dc uintptr, x, y int32, s string, font uintptr, fg uintptr) {
	units := syscall.StringToUTF16(s)
	pSelectObject.Call(dc, font)
	pSetTextColor.Call(dc, fg)
	pSetBkMode.Call(dc, colorTransparent)
	pTextOut.Call(dc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)-1))
	runtime.KeepAlive(units)
}
func paintRect(dc uintptr, x, y, w, h int32, brush uintptr) {
	r := rect{x, y, x + w, y + h}
	pFillRect.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
}

var appBlueBrush uintptr

func paintMain(hwnd uintptr) {
	var ps paintStruct
	dc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	paintRect(dc, 0, 0, app.width, app.height, app.bgBrush)
	paintRect(dc, 0, 0, app.width, 69, app.headerBrush)
	paintRect(dc, 0, 0, 3, 69, appBlueBrush)
	if app.icon != 0 {
		pDrawIconEx.Call(dc, 17, 17, app.icon, 34, 34, 0, 0, 3)
	}
	drawText(dc, 60, 17, "WPS 图片转 Excel", app.fontTitle, color(24, 43, 70))
	drawText(dc, 18, 184, "文件列表", app.fontHeading, color(34, 49, 69))
	totalPending := pendingCount()
	drawText(dc, app.width-257, 188, fmt.Sprintf("待转换 %d  ·  成功 %d  ·  失败 %d", totalPending, app.success, app.failure), app.fontSmall, color(91, 105, 125))
	paintRect(dc, 0, app.height-35, app.width, 35, app.headerBrush)
	status := app.status
	if status == "" {
		status = "就绪 · 先导入文件，设置选项，然后点击开始转换"
	}
	runes := []rune(status)
	if len(runes) > 95 {
		status = string(runes[:95]) + "…"
	}
	drawText(dc, 18, app.height-25, status, app.fontSmall, color(81, 100, 128))
}
func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
func layout() {
	if app.hwnd == 0 {
		return
	}
	w, h := app.width, app.height
	if w < 700 {
		w = 700
	}
	if h < 420 {
		h = 420
	}
	bounds := []struct {
		hwnd       uintptr
		x, y, w, h int32
	}{
		{app.pick, 18, 85, 112, 30}, {app.pickFolder, 140, 85, 112, 30},
		{app.start, 262, 85, 115, 30}, {app.clear, 387, 85, 90, 30},
		{app.help, w - 144, 20, 58, 26}, {app.about, w - 76, 20, 58, 26},
		{app.uniform, 18, 133, 295, 28}, {app.widthCombo, 310, 131, 100, 180},
		{app.grow, 430, 133, 265, 28},
		{app.list, 18, 210, w - 36, h - 255},
	}
	for _, b := range bounds {
		if b.hwnd != 0 {
			pMoveWindow.Call(b.hwnd, uintptr(b.x), uintptr(b.y), uintptr(b.w), uintptr(b.h), 1)
		}
	}
	if app.list != 0 {
		callSend(app.list, lvmSetColumnWidth, 3, uintptr(max32(145, w-36-225-75-66-128-18)))
	}
	pInvalidateRect.Call(app.hwnd, 0, 1)
}
func column(i int32, name string, width int32) {
	p := utf(name)
	c := lvColumn{Mask: lvcfFmt | lvcfWidth | lvcfText, Cx: width, Text: p}
	callSend(app.list, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&c)))
	runtime.KeepAlive(p)
}
func setCell(row, sub int, s string) {
	p := utf(s)
	item := lvItem{ISubItem: int32(sub), Text: p}
	callSend(app.list, lvmSetItemTextW, uintptr(row), uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(p)
}
func rowText(i int) {
	if i < 0 || i >= len(app.records) {
		return
	}
	r := app.records[i]
	setCell(i, 0, filepath.Base(r.Input))
	setCell(i, 1, r.Status)
	if r.Status == "成功" {
		setCell(i, 2, fmt.Sprintf("%d", r.Images))
	} else {
		setCell(i, 2, "—")
	}
	if r.Output != "" {
		setCell(i, 3, r.Output)
	} else if r.Status == "失败" {
		setCell(i, 3, r.Detail)
	} else {
		setCell(i, 3, r.Input)
	}
	setCell(i, 4, r.At)
}
func insertRow(r record) int {
	row := len(app.records)
	app.records = append(app.records, r)
	p := utf(filepath.Base(r.Input))
	item := lvItem{Mask: lvifText, IItem: int32(row), Text: p}
	rv := callSend(app.list, lvmInsertItemW, 0, uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(p)
	if int32(rv) < 0 {
		return -1
	}
	rowText(row)
	updateCounters()
	return row
}
func rebuildList() {
	callSend(app.list, lvmDeleteAllItems, 0, 0)
	prev := app.records
	app.records = nil
	for _, r := range prev {
		insertRow(r)
	}
	updateActions()
}
func selected() int {
	x := callSend(app.list, lvmGetNextItem, ^uintptr(0), lvniSelected)
	if int32(x) < 0 {
		return -1
	}
	return int(x)
}
func pendingCount() int {
	n := 0
	for _, r := range app.records {
		if r.Status == "待转换" {
			n++
		}
	}
	return n
}
func updateCounters() {
	app.success, app.failure = 0, 0
	for _, r := range app.records {
		if r.Status == "成功" {
			app.success++
		}
		if r.Status == "失败" {
			app.failure++
		}
	}
	updateActions()
	pInvalidateRect.Call(app.hwnd, 0, 0)
}
func updateActions() {
	if app.start != 0 {
		enabled := uintptr(0)
		if !app.running && pendingCount() > 0 {
			enabled = 1
		}
		pEnableWindow.Call(app.start, enabled)
	}
	if app.pick != 0 {
		enabled := uintptr(1)
		if app.running {
			enabled = 0
		}
		for _, control := range []uintptr{app.pick, app.pickFolder, app.clear, app.uniform, app.widthCombo, app.grow} {
			if control != 0 {
				pEnableWindow.Call(control, enabled)
			}
		}
	}
	if app.grow != 0 && !app.running && app.uniform != 0 && callSend(app.uniform, bmGetCheck, 0, 0) == 1 {
		pEnableWindow.Call(app.grow, 0)
	}
	if app.widthCombo != 0 && !app.running && app.uniform != 0 && callSend(app.uniform, bmGetCheck, 0, 0) != 1 {
		pEnableWindow.Call(app.widthCombo, 0)
	}
}
func recentHistoryPath() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir, _ = os.UserConfigDir()
	}
	return filepath.Join(dir, "WPSImageToExcel", "recent.json")
}
func loadHistory() {
	data, e := os.ReadFile(recentHistoryPath())
	if e != nil {
		return
	}
	var items []record
	if json.Unmarshal(data, &items) != nil {
		return
	}
	if len(items) > maxHistory {
		items = items[len(items)-maxHistory:]
	}
	for _, r := range items {
		if r.Status == "处理中" {
			r.Status = "待转换"
		}
		insertRow(r)
	}
}
func saveHistory() {
	items := append([]record(nil), app.records...)
	if len(items) > maxHistory {
		items = items[len(items)-maxHistory:]
	}
	for i := range items {
		if items[i].Status == "处理中" {
			items[i].Status = "待转换"
		}
	}
	data, e := json.MarshalIndent(items, "", "  ")
	if e != nil {
		return
	}
	dest := recentHistoryPath()
	if os.MkdirAll(filepath.Dir(dest), 0700) != nil {
		return
	}
	tmp := dest + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		if os.Rename(tmp, dest) != nil {
			os.Remove(tmp)
		}
	}
}
func shellOpen(path string) {
	if path == "" {
		message("没有可打开的文件。 ")
		return
	}
	if _, e := os.Stat(path); e != nil {
		message("找不到文件：\n" + path)
		return
	}
	verb, file := utf("open"), utf(path)
	ret, _, _ := pShellExecute.Call(app.hwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, swShow)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(file)
	if ret <= 32 {
		message(fmt.Sprintf("无法打开文件（错误 %d）：\n%s", ret, path))
	}
}
func infoMessage(s string) {
	a, b := utf(s), utf("WPS 图片转 Excel")
	pMessageBox.Call(app.hwnd, uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x40)
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
}
func askYesNo(text string) bool {
	a, b := utf(text), utf("确认操作")
	r, _, _ := pMessageBox.Call(app.hwnd, uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x30|0x4|0x100)
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
	return r == 6
}
func filePicker(owner uintptr) ([]string, error) {
	buf := make([]uint16, 65536)
	filter := multiStringUTF16("Excel 文件 (*.xlsx;*.xlsm)", "*.xlsx;*.xlsm", "所有文件 (*.*)", "*.*")
	title := utf("导入 WPS Excel 原始文件（可多选）")
	ofn := openFileName{LStructSize: uint32(unsafe.Sizeof(openFileName{})), HwndOwner: owner,
		LpstrFilter: &filter[0], LpstrFile: &buf[0], NMaxFile: uint32(len(buf)), LpstrTitle: title,
		Flags: 0x00080000 | 0x00000200 | 0x00001000 | 0x00000800 | 0x00000008}
	ok, _, _ := pGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(title)
	runtime.KeepAlive(buf)
	if ok == 0 {
		code, _, _ := pCommDlgExtendedError.Call()
		if code != 0 {
			return nil, fmt.Errorf("Windows 文件选择错误 0x%X", code)
		}
		return nil, nil
	}
	var v []string
	start := 0
	for i, c := range buf {
		if c != 0 {
			continue
		}
		if i == start {
			break
		}
		v = append(v, syscall.UTF16ToString(buf[start:i]))
		start = i + 1
	}
	if len(v) == 0 {
		return nil, fmt.Errorf("未读取到文件")
	}
	if len(v) == 1 {
		return v, nil
	}
	ret := make([]string, 0, len(v)-1)
	for _, name := range v[1:] {
		ret = append(ret, filepath.Join(v[0], name))
	}
	return ret, nil
}
func folderPicker() (string, error) {
	title := utf("选择含 WPS Excel 文件的文件夹（包括子文件夹）")
	display := make([]uint16, 260)
	bi := browseInfo{HwndOwner: app.hwnd, PszDisplayName: &display[0], LpszTitle: title, UlFlags: 0x1 | 0x10 | 0x40}
	pidl, _, _ := pSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	runtime.KeepAlive(title)
	if pidl == 0 {
		return "", nil
	}
	defer pCoTaskMemFree.Call(pidl)
	buf := make([]uint16, 32768)
	ok, _, _ := pSHGetPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return "", fmt.Errorf("无法获取文件夹路径")
	}
	return syscall.UTF16ToString(buf), nil
}
func draggedFiles(drop uintptr) []string {
	count, _, _ := pDragQueryFile.Call(drop, 0xFFFFFFFF, 0, 0)
	items := make([]string, 0, int(count))
	for i := uintptr(0); i < count; i++ {
		n, _, _ := pDragQueryFile.Call(drop, i, 0, 0)
		if n == 0 {
			continue
		}
		buf := make([]uint16, n+1)
		got, _, _ := pDragQueryFile.Call(drop, i, uintptr(unsafe.Pointer(&buf[0])), n+1)
		if got > 0 {
			items = append(items, syscall.UTF16ToString(buf[:got]))
		}
	}
	pDragFinish.Call(drop)
	return items
}
func validExcel(path string) bool {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".xlsx" && ext != ".xlsm" {
		return false
	}
	if strings.HasPrefix(base, "~$") {
		return false
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	// Do not repeatedly import this tool's generated output.
	return !strings.Contains(strings.ToLower(stem), strings.ToLower("_Office兼容"))
}
func expandImports(paths []string) ([]string, error) {
	const maxItems = 3000
	unique := map[string]bool{}
	result := make([]string, 0)
	add := func(s string) error {
		if !validExcel(s) {
			return nil
		}
		p, e := filepath.Abs(s)
		if e != nil {
			return nil
		}
		key := strings.ToLower(p)
		if !unique[key] {
			unique[key] = true
			result = append(result, p)
		}
		if len(result) >= maxItems {
			return fmt.Errorf("文件数量超过 %d，已停止扫描，请缩小导入范围", maxItems)
		}
		return nil
	}
	for _, input := range paths {
		input = strings.Trim(strings.TrimSpace(input), "\"")
		stat, e := os.Stat(input)
		if e != nil {
			continue
		}
		if !stat.IsDir() {
			if e := add(input); e != nil {
				return result, e
			}
			continue
		}
		e = filepath.Walk(input, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			return add(path)
		})
		if e != nil {
			return result, e
		}
	}
	sort.Strings(result)
	return result, nil
}
func addToQueue(paths []string) {
	if app.running {
		message("转换过程中不能导入文件。 ")
		return
	}
	files, err := expandImports(paths)
	if err != nil {
		message(err.Error())
	}
	found := map[string]int{}
	for i, r := range app.records {
		found[strings.ToLower(r.Input)] = i
	}
	added, skipped := 0, 0
	for _, p := range files {
		if idx, exists := found[strings.ToLower(p)]; exists {
			if app.records[idx].Status == "待转换" {
				skipped++
				continue
			}
			// Already processed records remain in history. Use '重新转换' from menu.
			skipped++
			continue
		}
		index := insertRow(record{Input: p, Status: "待转换", At: time.Now().Format("01-02 15:04")})
		if index >= 0 {
			found[strings.ToLower(p)] = index
			added++
		}
	}
	app.status = fmt.Sprintf("已导入 %d 个，跳过 %d 个重复文件；点击“开始转换”执行。", added, skipped)
	if added == 0 && len(files) == 0 && err == nil {
		message("没有找到可导入的 XLSX / XLSM 文件。")
	}
	saveHistory()
	updateCounters()
}
func currentOptions() convertOptions {
	opts := convertOptions{enlargeImageRows: callSend(app.grow, bmGetCheck, 0, 0) == 1}
	if callSend(app.uniform, bmGetCheck, 0, 0) == 1 {
		opts.uniformColumnWidth = true
		opts.enlargeImageRows = false
		n := int(callSend(app.widthCombo, cbGetCurSel, 0, 0))
		if n < 0 || n > 3 {
			n = 0
		}
		opts.widthPercent = []int{100, 90, 80, 70}[n]
	}
	return opts
}
func report(e event) { events <- e; pPostMessage.Call(app.hwnd, wmApp+1, 0, 0) }
func startProcess() {
	if app.running {
		return
	}
	var files []string
	var indices []int
	for i, r := range app.records {
		if r.Status == "待转换" {
			files = append(files, r.Input)
			indices = append(indices, i)
		}
	}
	if len(files) == 0 {
		message("请先导入文件，或右键选择“重新转换”。")
		return
	}
	opts := currentOptions()
	app.running = true
	for _, i := range indices {
		app.records[i].Status = "处理中"
		rowText(i)
	}
	app.status = fmt.Sprintf("正在转换 %d 个 Excel 文件…", len(files))
	updateCounters()
	go func() {
		defer func() {
			if x := recover(); x != nil {
				report(event{status: fmt.Sprintf("程序内部异常：%v\n%s", x, string(debug.Stack()))})
			}
			report(event{done: true})
		}()
		for k, input := range files {
			out := convertWorkbookWithOptions(input, opts)
			if out.err != nil {
				report(event{index: indices[k], status: "失败", detail: out.err.Error()})
			} else {
				report(event{index: indices[k], status: "成功", images: out.converted, output: out.output,
					detail: fmt.Sprintf("转换 %d 张图片，未匹配 %d 张", out.converted, out.unmatched)})
			}
		}
	}()
}
func onProgress() {
	for {
		select {
		case e := <-events:
			if e.done {
				app.running = false
				app.status = fmt.Sprintf("转换完成 · 列表成功 %d · 失败 %d", app.success, app.failure)
				saveHistory()
				updateCounters()
			} else if e.status == "成功" || e.status == "失败" {
				if e.index >= 0 && e.index < len(app.records) {
					r := &app.records[e.index]
					r.Status = e.status
					r.Images = e.images
					r.Output = e.output
					r.Detail = e.detail
					r.At = time.Now().Format("01-02 15:04")
					rowText(e.index)
					app.status = fmt.Sprintf("%s：%s · %s", e.status, filepath.Base(r.Input), e.detail)
					updateCounters()
					saveHistory()
				}
			} else if e.status != "" {
				app.status = e.status
				message(e.status)
			}
		default:
			pInvalidateRect.Call(app.hwnd, 0, 0)
			return
		}
	}
}
func openSelected(kind int) {
	i := selected()
	if i < 0 || i >= len(app.records) {
		message("请先选中一条文件记录。 ")
		return
	}
	r := app.records[i]
	switch kind {
	case menuOpenResult:
		if r.Output == "" {
			message("这条记录还没有转换结果。 ")
			return
		}
		shellOpen(r.Output)
	case menuOpenSource:
		shellOpen(r.Input)
	case menuReveal:
		target := r.Output
		if target == "" {
			target = r.Input
		}
		shellOpen(filepath.Dir(target))
	}
}
func renamePicker(old string) (string, error) {
	buf := make([]uint16, 2048)
	file := syscall.StringToUTF16(filepath.Base(old))
	copy(buf, file)
	filter := multiStringUTF16("Excel 文件 (*.xlsx;*.xlsm)", "*.xlsx;*.xlsm")
	dir := utf(filepath.Dir(old))
	title := utf("重命名转换结果（请选择同一文件夹中的新文件名）")
	ofn := openFileName{LStructSize: uint32(unsafe.Sizeof(openFileName{})), HwndOwner: app.hwnd,
		LpstrFilter: &filter[0], LpstrFile: &buf[0], NMaxFile: uint32(len(buf)), LpstrInitialDir: dir, LpstrTitle: title,
		Flags: 0x00000800 | 0x00000008 | 0x00800000}
	ok, _, _ := pGetSaveFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(dir)
	runtime.KeepAlive(title)
	runtime.KeepAlive(buf)
	if ok == 0 {
		code, _, _ := pCommDlgExtendedError.Call()
		if code != 0 {
			return "", fmt.Errorf("重命名对话框错误 0x%X", code)
		}
		return "", nil
	}
	return syscall.UTF16ToString(buf), nil
}
func performMenu(id int) {
	idx := selected()
	if idx < 0 || idx >= len(app.records) {
		return
	}
	r := &app.records[idx]
	if app.running {
		message("转换过程中请勿修改文件列表。")
		return
	}
	switch id {
	case menuOpenResult, menuOpenSource, menuReveal:
		openSelected(id)
	case menuRemove:
		app.records = append(app.records[:idx], app.records[idx+1:]...)
		rebuildList()
		saveHistory()
		app.status = "已从列表移除；磁盘文件未删除"
	case menuRequeue:
		if _, e := os.Stat(r.Input); e != nil {
			message("原文件不存在：\n" + r.Input)
			return
		}
		r.Status = "待转换"
		r.Output = ""
		r.Images = 0
		r.Detail = ""
		r.At = time.Now().Format("01-02 15:04")
		rowText(idx)
		saveHistory()
		updateCounters()
	case menuRename:
		if r.Output == "" {
			message("没有可重命名的输出文件。 ")
			return
		}
		newName, e := renamePicker(r.Output)
		if e != nil {
			message(e.Error())
			return
		}
		if newName == "" || strings.EqualFold(newName, r.Output) {
			return
		}
		newName = filepath.Clean(newName)
		if !strings.EqualFold(filepath.Dir(newName), filepath.Dir(r.Output)) {
			message("仅支持在原文件夹内重命名。 ")
			return
		}
		if !strings.EqualFold(filepath.Ext(newName), filepath.Ext(r.Output)) {
			message("重命名时请保留原扩展名。 ")
			return
		}
		if _, e = os.Stat(newName); e == nil {
			message("同名文件已存在，无法覆盖。 ")
			return
		}
		if e = os.Rename(r.Output, newName); e != nil {
			message("重命名失败：" + e.Error())
			return
		}
		r.Output = newName
		rowText(idx)
		saveHistory()
		app.status = "已重命名转换结果：" + filepath.Base(newName)
	case menuDeleteResult:
		if r.Output == "" {
			message("没有可删除的转换结果。 ")
			return
		}
		if !askYesNo("仅删除下面这个转换结果，WPS 原文件不受影响：\n" + r.Output + "\n\n确定删除吗？") {
			return
		}
		if e := os.Remove(r.Output); e != nil {
			message("删除失败：" + e.Error())
			return
		}
		r.Output = ""
		r.Status = "结果已删除"
		r.Images = 0
		r.Detail = "已删除输出文件"
		rowText(idx)
		saveHistory()
		updateCounters()
		app.status = "已删除转换结果；原文件保持不变"
	}
}
func addMenu(h uintptr, id int, name string, enabled bool) {
	txt := utf(name)
	flags := uintptr(0)
	if !enabled {
		flags = 0x1
	}
	pAppendMenu.Call(h, flags, uintptr(id), uintptr(unsafe.Pointer(txt)))
	runtime.KeepAlive(txt)
}
func contextMenu() {
	pt := point{}
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	local := pt
	pScreenToClient.Call(app.list, uintptr(unsafe.Pointer(&local)))
	hit := lvHitTest{Pt: local, IItem: -1}
	callSend(app.list, lvmHitTest, 0, uintptr(unsafe.Pointer(&hit)))
	if hit.IItem >= 0 && int(hit.IItem) < len(app.records) {
		clear := lvItem{State: 0, StateMask: lvniSelected}
		callSend(app.list, lvmSetItemState, ^uintptr(0), uintptr(unsafe.Pointer(&clear)))
		chosen := lvItem{State: lvniSelected, StateMask: lvniSelected}
		callSend(app.list, lvmSetItemState, uintptr(hit.IItem), uintptr(unsafe.Pointer(&chosen)))
	}
	idx := selected()
	if idx < 0 || idx >= len(app.records) {
		return
	}
	r := app.records[idx]
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	hasOutput := r.Output != ""
	addMenu(menu, menuOpenResult, "打开转换结果", hasOutput)
	addMenu(menu, menuOpenSource, "打开原始文件", true)
	addMenu(menu, menuReveal, "打开所在文件夹", true)
	pAppendMenu.Call(menu, 0x800, 0, 0)
	addMenu(menu, menuRequeue, "重新转换", true)
	addMenu(menu, menuRename, "重命名结果文件…", hasOutput)
	addMenu(menu, menuDeleteResult, "删除输出文件…", hasOutput)
	pAppendMenu.Call(menu, 0x800, 0, 0)
	addMenu(menu, menuRemove, "从列表移除（不删文件）", true)
	// Return command instead of dispatching WM_COMMAND asynchronously.
	chosen, _, _ := pTrackPopupMenu.Call(menu, 0x0100|0x0002, uintptr(pt.X), uintptr(pt.Y), 0, app.hwnd, 0)
	if chosen != 0 {
		performMenu(int(chosen))
	}
}
func wndProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case wmCreate:
		app.hwnd = hwnd
		app.font = gdiFont(-14, 400)
		app.fontSmall = gdiFont(-12, 400)
		app.fontTitle = gdiFont(-22, 650)
		app.fontHeading = gdiFont(-16, 650)
		inst, _, _ := pGetModuleHandle.Call(0)
		app.icon, _, _ = pLoadImage.Call(inst, 1, 1, 48, 48, 0)
		style := uint32(wsChild | wsVisible | wsTabstop | bsPushButton)
		app.pick = control("BUTTON", "＋ 导入文件", style, 0, 0, 0, 0, 0, cmdPick)
		app.pickFolder = control("BUTTON", "＋ 导入文件夹", style, 0, 0, 0, 0, 0, cmdPickFolder)
		app.start = control("BUTTON", "▶ 开始转换", style, 0, 0, 0, 0, 0, cmdStart)
		app.clear = control("BUTTON", "清空列表", style, 0, 0, 0, 0, 0, cmdClear)
		app.help = control("BUTTON", "帮助", style, 0, 0, 0, 0, 0, cmdHelp)
		app.about = control("BUTTON", "关于", style, 0, 0, 0, 0, 0, cmdAbout)
		app.uniform = control("BUTTON", "统一图片宽度（按原表列宽）", wsChild|wsVisible|wsTabstop|bsAutoCheckBox, 0, 0, 0, 0, 0, cmdUniform)
		app.grow = control("BUTTON", "放大图片所在行（≥120 px）", wsChild|wsVisible|wsTabstop|bsAutoCheckBox, 0, 0, 0, 0, 0, cmdEnlarge)
		app.widthCombo = control("COMBOBOX", "", wsChild|wsVisible|wsTabstop|wsVscroll|0x0003, 0, 0, 0, 0, 0, cmdWidth)
		for _, label := range []string{"100% 列宽", "90% 列宽", "80% 列宽", "70% 列宽"} {
			val := utf(label)
			callSend(app.widthCombo, cbAddString, 0, uintptr(unsafe.Pointer(val)))
			runtime.KeepAlive(val)
		}
		callSend(app.widthCombo, cbSetCurSel, 0, 0)
		callSend(app.uniform, bmSetCheck, 1, 0)
		app.list = control("SysListView32", "", wsChild|wsVisible|wsTabstop|wsBorder|lvsReport|lvsSingleSel|lvsShowSelAlways, wsExClientEdge, 0, 0, 0, 0, 1201)
		if app.list == 0 {
			message("无法创建文件列表控件。")
			return 0
		}
		listWndCallback = syscall.NewCallback(listWindowProc)
		originalListWndProc, _, _ = pSetWindowLongPtr.Call(app.list, ^uintptr(3), listWndCallback)
		if originalListWndProc != 0 {
			pDragAcceptFiles.Call(app.list, 1)
		}
		explorer := utf("Explorer")
		pSetWindowTheme.Call(app.list, uintptr(unsafe.Pointer(explorer)), 0)
		runtime.KeepAlive(explorer)
		callSend(app.list, lvmSetExtendedListViewStyle, lvsExFullRowSelect|lvsExDoubleBuffer, lvsExFullRowSelect|lvsExDoubleBuffer)
		column(0, "原文件", 225)
		column(1, "状态", 75)
		column(2, "图片", 66)
		column(3, "输出路径 / 失败原因", 240)
		column(4, "时间", 128)
		var r rect
		pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		app.width = r.Right
		app.height = r.Bottom
		layout()
		loadHistory()
		pDragAcceptFiles.Call(hwnd, 1)
		updateCounters()
		return 0
	case wmSize:
		app.width = int32(uint16(l & 0xFFFF))
		app.height = int32(uint16((l >> 16) & 0xFFFF))
		layout()
		return 0
	case wmPaint:
		paintMain(hwnd)
		return 0
	case wmCommand:
		id := int(w & 0xFFFF)
		switch id {
		case cmdPick:
			files, e := filePicker(hwnd)
			if e != nil {
				message(e.Error())
			} else if len(files) > 0 {
				addToQueue(files)
			}
		case cmdPickFolder:
			folder, e := folderPicker()
			if e != nil {
				message(e.Error())
			} else if folder != "" {
				addToQueue([]string{folder})
			}
		case cmdStart:
			startProcess()
		case cmdClear:
			if app.running {
				message("转换过程中不能清空列表。 ")
				return 0
			}
			if len(app.records) > 0 && !askYesNo("清空全部文件记录？\n不会删除磁盘上的任何 Excel 文件。") {
				return 0
			}
			app.records = nil
			rebuildList()
			saveHistory()
			app.status = "文件列表已清空"
			updateCounters()
		case cmdUniform:
			updateActions()
		case cmdAbout:
			infoMessage("WPS 图片转 Excel · v" + version + "\n\n适用系统：Windows 64 位\n功能：将 WPS DISPIMG 图片转为标准 Excel 图片\n\n保留 2.7 验证过的核心布局算法。\n新增导入队列、统一列宽、右键文件管理。\n本工具不需要安装 WPS 或 Python。")
		case cmdHelp:
			infoMessage("使用步骤：\n1. 导入 Excel 文件或文件夹，也可直接拖入。\n2. 设置图片宽度和行高选项。\n3. 点击“开始转换”。\n4. 双击成功记录打开转换结果。\n5. 右键可打开、重命名、删除输出文件等。\n\n统一图片宽度：100% 对齐所在列的可用宽度，并自动增加必要的行高。\n关闭统一宽度：使用 2.7 验证过的原布局模式。\n删除输出文件不会删除 WPS 原文件。")
		}
		return 0
	case wmNotify:
		if l != 0 {
			h := (*nmHdr)(unsafe.Pointer(l))
			if h.HwndFrom == app.list {
				if int32(h.Code) == nmDblClick {
					openSelected(menuOpenResult)
					return 0
				}

			}
		}
		return 0
	case wmContextMenu:
		if w == app.list {
			contextMenu()
			return 0
		}
	case wmDropFiles:
		addToQueue(draggedFiles(w))
		return 0
	case wmApp + 1:
		onProgress()
		return 0
	case wmApp + 2:
		if len(os.Args) > 1 {
			addToQueue(os.Args[1:])
		}
		return 0
	case wmClose:
		if app.running {
			message("仍有文件正在转换，请完成后关闭程序。 ")
			return 0
		}
	case wmDestroy:
		saveHistory()
		for _, object := range []uintptr{app.font, app.fontSmall, app.fontTitle, app.fontHeading} {
			if object != 0 {
				pDeleteObject.Call(object)
			}
		}
		pPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := pDefWindowProc.Call(hwnd, uintptr(msg), w, l)
	return ret
}
func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		if x := recover(); x != nil {
			message(fmt.Sprintf("程序启动异常：%v", x))
		}
	}()
	cc := initCommonControlsEx{Size: uint32(unsafe.Sizeof(initCommonControlsEx{})), ICC: iccListViewClasses}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&cc)))
	// Folder selection's new-style shell dialog requires STA COM initialization.
	initCOM, _, _ := pCoInitializeEx.Call(0, 2)
	if initCOM == 0 || initCOM == 1 {
		defer pCoUninitialize.Call()
	}
	app.bgBrush, _, _ = pCreateSolidBrush.Call(color(250, 251, 253))
	app.headerBrush, _, _ = pCreateSolidBrush.Call(color(236, 243, 255))
	appBlueBrush, _, _ = pCreateSolidBrush.Call(color(38, 99, 235))
	inst, _, _ := pGetModuleHandle.Call(0)
	clsname := utf("WPSImageToExcelMainV29")
	cur, _, _ := pLoadCursor.Call(0, 32512)
	icon, _, _ := pLoadImage.Call(inst, 1, 1, 40, 40, 0)
	cls := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: syscall.NewCallback(wndProc), HInstance: inst,
		HCursor: cur, HIcon: icon, HIconSm: icon, HbrBackground: app.bgBrush, LpszClassName: clsname}
	if a, _, e := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&cls))); a == 0 {
		message(fmt.Sprintf("注册窗口失败：%v", e))
		return
	}
	title := utf("WPS 图片转 Excel")
	hwnd, _, e := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(clsname)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow|wsClipChildren|wsVisible,
		120, 100, 915, 575, 0, 0, inst, 0)
	runtime.KeepAlive(clsname)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		message(fmt.Sprintf("创建窗口失败：%v", e))
		return
	}
	app.hwnd = hwnd
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	if len(os.Args) > 1 {
		pPostMessage.Call(hwnd, wmApp+2, 0, 0)
	}
	var m winMsg
	for {
		ret, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || int32(ret) == -1 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

