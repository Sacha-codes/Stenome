package main

import (
	"database/sql"
	"fmt"
	"fyne.io/fyne/v2/app"
	"github.com/go-vgo/robotgo"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/sys/windows"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	lettersToWord = make(map[string]string)
	db            *sql.DB

	// Protects the snippet map if you later add live database reloading.
	snippetsMu sync.RWMutex

	// Prevents our own injected keystrokes from being processed again.
	isTyping bool
	typingMu sync.Mutex

	user32 = windows.NewLazySystemDLL("user32.dll")

	procSetWindowsHookExW = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHook = user32.NewProc("UnhookWindowsHookEx")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")

	keyboardHook windows.Handle
	hookCallback uintptr
)

// Windows constants.
const (
	WH_KEYBOARD_LL = 13

	WM_KEYDOWN    = 0x0100
	WM_KEYUP      = 0x0101
	WM_SYSKEYDOWN = 0x0104
	WM_SYSKEYUP   = 0x0105

	VK_BACK    = 0x08
	VK_TAB     = 0x09
	VK_RETURN  = 0x0D
	VK_SPACE   = 0x20
	VK_ESCAPE  = 0x1B
	VK_CAPITAL = 0x14

	HC_ACTION = 0

	// KBDLLHOOKSTRUCT flag indicating an injected event.
	LLKHF_INJECTED = 0x00000010
)

var (
	enabledMu          sync.RWMutex
	stenographyEnabled = true
)

func setStenographyEnabled(enabled bool) {
	enabledMu.Lock()
	stenographyEnabled = enabled
	enabledMu.Unlock()
}

func isStenographyEnabled() bool {
	enabledMu.RLock()
	defer enabledMu.RUnlock()
	return stenographyEnabled
}

// KBDLLHOOKSTRUCT is supplied by Windows for WH_KEYBOARD_LL.
type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// MSG is the Windows message structure.
type MSG struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct {
		X int32
		Y int32
	}
}

// SQLite_connect opens the SQLite database.
func SQLite_connect() {
	var err error

	appData, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}

	appDir := filepath.Join(appData, "Stenome")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		log.Fatal(err)
	}

	dbPath := filepath.Join(appDir, "user_settings.db")

	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	var tableExists bool
	err = db.QueryRow(`    SELECT EXISTS (
        SELECT 1 FROM sqlite_master
        WHERE type = 'table' AND name = 'snippets'
    )`).Scan(&tableExists)
	if err != nil {
	log.Fatal(err)
	}


	// Create the table if it doesn't already exist.
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS snippets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		shortcut TEXT NOT NULL UNIQUE,
		expansion TEXT NOT NULL
	)
	`)

	if !tableExists {
	_, err = db.Exec(`        INSERT INTO snippets (shortcut, expansion) VALUES
        ('idk', 'I don''t know'),
        ('brb', 'be right back'),
        ('ty', 'thank you!')
   	`)}

	if err != nil {
		log.Fatal(err)
	}
}

// SQL_extractor loads shortcut -> expansion mappings.
func SQL_extractor() error {
	rows, err := db.Query(`
		SELECT shortcut, expansion
		FROM snippets
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	snippetsMu.Lock()
	defer snippetsMu.Unlock()

	lettersToWord = make(map[string]string)

	for rows.Next() {
		var shortcut string
		var expansion string

		if err := rows.Scan(&shortcut, &expansion); err != nil {
			return err
		}

		lettersToWord[shortcut] = expansion
	}

	return rows.Err()

}

// SQL_boot initializes SQLite and loads snippets.
func SQL_boot() {
	SQLite_connect()

	if err := SQL_extractor(); err != nil {
		log.Fatal(err)
	}
}

// isDelimiter determines whether the key ends a word.
func isDelimiter(vk uint32) bool {
	switch vk {
	case VK_SPACE, VK_RETURN, VK_TAB:
		return true
	}

	return false

}

// isBackspace handles editing of the current buffer.
func isBackspace(vk uint32) bool {
	return vk == VK_BACK
}

// virtualKeyToString converts a virtual key to text using the current
// Windows keyboard layout.
func virtualKeyToString(vk uint32, scanCode uint32) string {
	var keyboardState [256]byte

	procGetKeyboardState := user32.NewProc("GetKeyboardState")
	procMapVirtualKeyExW := user32.NewProc("MapVirtualKeyExW")
	procGetKeyboardLayout := user32.NewProc("GetKeyboardLayout")
	procToUnicodeEx := user32.NewProc("ToUnicodeEx")

	ret, _, _ := procGetKeyboardState.Call(
		uintptr(unsafe.Pointer(&keyboardState[0])),
	)
	if ret == 0 {
		return ""
	}

	layout, _, _ := procGetKeyboardLayout.Call(0)

	// If scan code wasn't supplied, obtain it from the virtual key.
	if scanCode == 0 {
		result, _, _ := procMapVirtualKeyExW.Call(
			uintptr(vk),
			0,
			layout,
		)
		scanCode = uint32(result)
	}

	var output [8]uint16

	n, _, _ := procToUnicodeEx.Call(
		uintptr(vk),
		uintptr(scanCode),
		uintptr(unsafe.Pointer(&keyboardState[0])),
		uintptr(unsafe.Pointer(&output[0])),
		uintptr(len(output)),
		0,
		layout,
	)

	if int32(n) <= 0 {
		return ""
	}

	return syscall.UTF16ToString(output[:n])

}

// expand replaces a shortcut with its expansion.
//
// IMPORTANT:
// The delimiter itself has already been physically delivered to the
// foreground application. We therefore only replace the shortcut text.
func expand(shortcut string) {

	if shortcut == "" {
		return
	}

	snippetsMu.RLock()
	word, ok := lettersToWord[shortcut]
	snippetsMu.RUnlock()

	if !ok {
		return
	}

	typingMu.Lock()

	if isTyping {
		typingMu.Unlock()
		return
	}

	isTyping = true
	typingMu.Unlock()

	defer func() {
		typingMu.Lock()
		isTyping = false
		typingMu.Unlock()
	}()

	// Remove the shortcut from the target application.
	for range shortcut {
		robotgo.KeyTap("backspace")
	}
	robotgo.KeyTap("backspace")

	// Insert the expansion.
	robotgo.Type(word)
	robotgo.KeyTap("space") // Add a space after the expansion
}

// keyboardHookProc is the Windows low-level keyboard callback.
func keyboardHookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode != HC_ACTION {
		ret, _, _ := procCallNextHookEx.Call(
			0,
			uintptr(nCode),
			wParam,
			lParam,
		)
		return ret
	}

	data := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))

	// Ignore keyboard events generated by robotgo.
	if data.Flags&LLKHF_INJECTED != 0 {
		ret, _, _ := procCallNextHookEx.Call(
			0,
			uintptr(nCode),
			wParam,
			lParam,
		)
		return ret
	}

	// Only process key-down events.
	if wParam != WM_KEYDOWN && wParam != WM_SYSKEYDOWN {
		ret, _, _ := procCallNextHookEx.Call(
			0,
			uintptr(nCode),
			wParam,
			lParam,
		)
		return ret
	}

	// The actual buffer is kept outside this callback.
	processKey(data.VkCode, data.ScanCode)

	ret, _, _ := procCallNextHookEx.Call(
		0,
		uintptr(nCode),
		wParam,
		lParam,
	)

	return ret

}

var (
	bufferMu sync.Mutex
	buffer   string
)

// processKey receives a single physical key event.
func processKey(vk uint32, scanCode uint32) {
	typingMu.Lock()
	typing := isTyping
	typingMu.Unlock()

	if !isStenographyEnabled() {
		return
	}

	if typing {
		return
	}

	bufferMu.Lock()
	defer bufferMu.Unlock()

	// Escape clears the current shortcut buffer.
	if vk == VK_CAPITAL {
		buffer = ""
		robotgo.KeyTap("Capslock") // Toggle Caps Lock to avoid affecting the user
		return
	}

	// Backspace removes the last character from the buffer.
	if isBackspace(vk) {
		if len(buffer) > 0 {
			runes := []rune(buffer)
			buffer = string(runes[:len(runes)-1])
		}
		return
	}

	// A delimiter means the user has finished typing the word.
	if isDelimiter(vk) {
		shortcut := strings.TrimSpace(buffer)
		buffer = ""

		// Do the replacement asynchronously so the low-level hook
		// returns quickly.
		if shortcut != "" {
			go expand(shortcut)
		}

		return
	}

	// Convert the key to the character produced by the current
	// Windows keyboard layout.
	text := virtualKeyToString(vk, scanCode)

	if text != "" {
		buffer += text
	}

}

// installKeyboardHook installs WH_KEYBOARD_LL.
func installKeyboardHook() error {
	hookCallback = windows.NewCallback(keyboardHookProc)

	handle, _, err := procSetWindowsHookExW.Call(
		WH_KEYBOARD_LL,
		hookCallback,
		0,
		0,
	)

	if handle == 0 {
		return fmt.Errorf("SetWindowsHookExW failed: %w", err)
	}

	keyboardHook = windows.Handle(handle)
	return nil
}

// uninstallKeyboardHook removes the global keyboard hook.
func uninstallKeyboardHook() {
	if keyboardHook != 0 {
		procUnhookWindowsHook.Call(uintptr(keyboardHook))
		keyboardHook = 0
	}
}

// messageLoop keeps the Windows hook alive.
func messageLoop() error {
	var msg MSG

	for {
		ret, _, err := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)

		switch int32(ret) {
		case -1:
			return fmt.Errorf("GetMessageW failed: %w", err)

		case 0:
			return nil

		default:
			procTranslateMessage.Call(
				uintptr(unsafe.Pointer(&msg)),
			)

			procDispatchMessageW.Call(
				uintptr(unsafe.Pointer(&msg)),
			)
		}
	}

}

func main() {
	SQL_boot()
	defer db.Close()

	if err := installKeyboardHook(); err != nil {
		log.Fatal(err)
	}
	defer uninstallKeyboardHook()

	go func() {
		if err := messageLoop(); err != nil {
			log.Println("Keyboard message loop:", err)
		}
	}()

	a := app.New()
	window := createUI(a)
	window.ShowAndRun()
}

func addSnippet(shortcut, expansion string) error {
	_, err := db.Exec(`
		INSERT INTO snippets (shortcut, expansion)
		VALUES (?, ?)
	`, shortcut, expansion)
	if err != nil {
		return err
	}

	snippetsMu.Lock()
	lettersToWord[shortcut] = expansion
	snippetsMu.Unlock()

	return nil
}

func deleteSnippet(shortcut string) error {
	_, err := db.Exec(
		`DELETE FROM snippets WHERE shortcut = ?`,
		shortcut,
	)
	if err != nil {
		return err
	}

	snippetsMu.Lock()
	delete(lettersToWord, shortcut)
	snippetsMu.Unlock()

	return nil
}
