//go:build windows

package ui

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	"golang.org/x/sys/windows"
)

// У fyne нет события «окно свернули»: и glfw, и сам fyne такого колбэка
// не отдают. Поэтому состояние окна опрашивается через Win32 — по свёртыванию
// окно прячется в трей.
var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsIconic                 = user32.NewProc("IsIconic")
	procIsWindow                 = user32.NewProc("IsWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindow                = user32.NewProc("GetWindow")
)

const (
	swRestore = 9
	gwOwner   = 4

	// minimizePollPeriod — период опроса состояния окна. Чаще не нужно:
	// реакция и так выглядит мгновенной.
	minimizePollPeriod = 300 * time.Millisecond
)

// hwnd — дескриптор собственного окна; заполняется, когда окно впервые создано.
var hwnd atomic.Uintptr

// watchWindowState следит за сворачиванием окна и за блокировкой сеанса.
// Обе проверки дешёвые и делаются в одной горутине, чтобы не плодить таймеры.
func (a *App) watchWindowState() {
	go func() {
		ticker := time.NewTicker(minimizePollPeriod)
		defer ticker.Stop()

		// Исходное состояние сеанса только запоминаем: реагировать нужно
		// на переходы, а не на факт «сейчас заблокировано».
		locked := sessionLocked()

		for {
			select {
			case <-a.done:
				return
			case <-ticker.C:
				locked = a.checkSession(locked)
				a.checkMinimized()
			}
		}
	}()
}

func (a *App) checkMinimized() {
	// Скрытое окно сворачивать не нужно. Проверка заодно снимает поток
	// повторных вызовов: свёрнутость мы намеренно не сбрасываем, поэтому
	// без неё IsIconic оставался бы истинным и после того, как окно уже убрано.
	if !a.windowVisible.Load() {
		return
	}

	h := hwnd.Load()

	// Пока окно ни разу не показывали, нативного окна не существует:
	// fyne создаёт его лениво, при первом Show.
	if h == 0 || !isWindow(h) {
		h = findOwnWindow()
		if h == 0 {
			return
		}
		hwnd.Store(h)
	}

	if !isIconic(h) {
		return
	}

	// Свёрнутость снимаем не здесь, а при следующем показе окна: иначе
	// пользователь увидит, как окно разворачивается и тут же исчезает.
	fyne.Do(a.hideToTray)
}

// restoreFromMinimized разворачивает окно, если оно было свёрнуто, и выводит
// его на передний план.
func (a *App) restoreFromMinimized() {
	h := hwnd.Load()
	if h == 0 || !isWindow(h) {
		return
	}
	if isIconic(h) {
		procShowWindow.Call(h, swRestore)
	}
	// Windows может отклонить смену активного окна, если сейчас работают
	// с другим приложением. Тогда окно просто мигнёт в панели задач —
	// это допустимо, отдельной обработки не требует.
	procSetForegroundWindow.Call(h)
}

// findOwnWindow ищет собственное окно верхнего уровня.
//
// Поиск по одному заголовку ненадёжен — так же может называться окно чужой
// программы, поэтому дополнительно сверяется идентификатор процесса.
func findOwnWindow() uintptr {
	var found uintptr
	self := windows.GetCurrentProcessId()

	cb := syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessID.Call(h, uintptr(unsafe.Pointer(&pid)))
		if pid != self {
			return 1 // продолжаем перебор
		}
		if owner, _, _ := procGetWindow.Call(h, gwOwner); owner != 0 {
			return 1 // дочернее окно нам не подходит
		}
		if windowText(h) != windowTitle {
			return 1
		}
		found = h
		return 0 // нашли — перебор можно прекращать
	})

	procEnumWindows.Call(cb, 0)
	return found
}

func windowText(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func isIconic(h uintptr) bool {
	r, _, _ := procIsIconic.Call(h)
	return r != 0
}

func isWindow(h uintptr) bool {
	r, _, _ := procIsWindow.Call(h)
	return r != 0
}
