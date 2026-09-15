//go:build windows

package ui

import (
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
)

// Блокировка экрана — отдельная забота именно для этого приложения: оно живёт
// сутками, и окно вполне может остаться открытым, когда сеанс заблокируется.
//
// Пока окно видимо, fyne продолжает его перерисовывать: секундный обратный
// отсчёт помечает холст «грязным», и цикл событий раз в секунду отправляет кадр
// в OpenGL. За часы работы с погашенным экраном контекст успевает стать
// недействительным, и после разблокировки окно оказывается живым только внешне:
// оно не перерисовывается и не реагирует на нажатия, помогает лишь перезапуск.
//
// Поэтому на время блокировки окно прячется. В fyne это ровно то, что нужно:
// drawSingleFrame пропускает невидимые окна (decideRepaint проверяет w.visible),
// то есть рисование прекращается полностью. После разблокировки окно
// возвращается на место.
var (
	procOpenInputDesktop = user32.NewProc("OpenInputDesktop")
	procCloseDesktop     = user32.NewProc("CloseDesktop")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
)

const (
	desktopSwitchDesktop = 0x0100

	// restoreCheckDelay — пауза перед проверкой, что окно действительно
	// вернулось после разблокировки.
	restoreCheckDelay = 1500 * time.Millisecond
)

// sessionLocked сообщает, заблокирован ли сеанс.
//
// Проверка косвенная, но не требует ни скрытого окна с обработчиком сообщений,
// ни подписки WTSRegisterSessionNotification: у заблокированного сеанса входным
// становится защищённый рабочий стол Winlogon, и OpenInputDesktop перестаёт его
// отдавать.
func sessionLocked() bool {
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopSwitchDesktop)
	if h == 0 {
		return true
	}
	procCloseDesktop.Call(h)
	return false
}

// checkSession отслеживает смену состояния сеанса и возвращает новое состояние.
func (a *App) checkSession(wasLocked bool) bool {
	locked := sessionLocked()
	if locked == wasLocked {
		return locked
	}

	if locked {
		a.onSessionLocked()
	} else {
		a.onSessionUnlocked()
	}
	return locked
}

func (a *App) onSessionLocked() {
	if !a.windowVisible.Load() {
		slog.Debug("сеанс заблокирован, окно и так скрыто")
		return
	}

	slog.Info("сеанс заблокирован — прячем окно, чтобы не рисовать в погашенный экран")
	a.hiddenByLock.Store(true)
	fyne.Do(a.hideToTray)
}

func (a *App) onSessionUnlocked() {
	// Возвращаем окно только если сами его убрали: если пользователь свернул
	// его в трей до блокировки, оно и должно остаться в трее.
	if !a.hiddenByLock.Swap(false) {
		slog.Debug("сеанс разблокирован, окно возвращать не нужно")
		return
	}

	slog.Info("сеанс разблокирован — возвращаем окно")
	fyne.Do(a.showWindow)

	// Убеждаемся, что окно действительно вернулось. После смены состояния
	// экрана GLFW может держать недействительный дескриптор окна, и тогда
	// Show молча не делает ничего — пользователю лучше узнать об этом сразу,
	// чем гадать, куда делось приложение.
	time.Sleep(restoreCheckDelay)
	if h := hwnd.Load(); h != 0 && !isWindowVisible(h) {
		slog.Error("окно не восстановилось после разблокировки")
		a.notify(windowTitle, "Не удалось вернуть окно на экран. Откройте его через меню в трее, а если не поможет — перезапустите приложение.")
	}
}

func isWindowVisible(h uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(h)
	return r != 0
}
