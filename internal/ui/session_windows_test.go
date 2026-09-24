//go:build windows

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"mover/internal/config"
)

// trayApp — тестовое приложение с треем. Его SetSystemTrayWindow повторяет
// побочный эффект настоящего драйвера fyne: перехват крестика подменяется
// голым Hide.
type trayApp struct {
	fyne.App
	win *interceptWindow
}

func (a *trayApp) NewWindow(title string) fyne.Window {
	a.win = &interceptWindow{Window: a.App.NewWindow(title)}
	return a.win
}

func (*trayApp) SetSystemTrayMenu(*fyne.Menu)    {}
func (*trayApp) SetSystemTrayIcon(fyne.Resource) {}

func (*trayApp) SetSystemTrayWindow(w fyne.Window) { w.SetCloseIntercept(w.Hide) }

// interceptWindow запоминает перехват крестика: тестовое окно fyne не умеет
// вызвать его само.
type interceptWindow struct {
	fyne.Window
	intercept func()
}

func (w *interceptWindow) SetCloseIntercept(f func()) {
	w.intercept = f
	w.Window.SetCloseIntercept(f)
}

// TestClosedWindowStaysInTrayAfterUnlock воспроизводит жалобу: окно убрали
// крестиком, сеанс заблокировали и разблокировали — окно должно остаться
// в трее, а не открыться само.
func TestClosedWindowStaysInTrayAfterUnlock(t *testing.T) {
	fake := &trayApp{App: test.NewApp()}
	t.Cleanup(func() { test.NewApp() })

	a := &App{
		fyneApp: fake,
		cfg:     config.Default(),
		done:    make(chan struct{}),
	}
	a.buildWindow()
	a.buildTray()
	a.windowVisible.Store(true)

	if fake.win.intercept == nil {
		t.Fatal("перехват крестика не установлен")
	}
	fake.win.intercept()

	if a.windowVisible.Load() {
		t.Fatal("после крестика окно считается видимым — блокировка вернёт его на экран")
	}

	a.onSessionLocked()
	a.onSessionUnlocked()

	if a.hiddenByLock.Load() {
		t.Error("окно из трея помечено как спрятанное блокировкой")
	}
	if a.windowVisible.Load() {
		t.Error("после разблокировки окно из трея снова показано")
	}
}
