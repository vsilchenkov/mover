package ui

import (
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"mover/internal/jiggler"
)

// trayRebuildDelay — пауза перед пересборкой меню, чтобы всплывающее меню
// успело закрыться: пересобирать его прямо из обработчика нажатия нельзя.
const trayRebuildDelay = 400 * time.Millisecond

// tray — состояние иконки в трее и её меню.
type tray struct {
	desk desktop.App
	menu *fyne.Menu

	itemAutorun *fyne.MenuItem

	// Последнее показанное состояние: менять иконку имеет смысл только
	// когда состояние действительно изменилось.
	shownRunning bool
	shownOnce    bool
}

// buildTray создаёт иконку в трее с контекстным меню.
func (a *App) buildTray() {
	desk, ok := a.fyneApp.(desktop.App)
	if !ok {
		// На платформе без трея приложение всё равно должно работать.
		slog.Warn("системный трей недоступен")
		return
	}
	a.tray.desk = desk

	a.tray.itemAutorun = fyne.NewMenuItem("Автозапуск с Windows", a.toggleAutorun)

	a.tray.menu = fyne.NewMenu(windowTitle,
		fyne.NewMenuItem("Запустить", a.start),
		fyne.NewMenuItem("Остановить", a.stop),
		fyne.NewMenuItemSeparator(),
		a.tray.itemAutorun,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Показать окно", a.showWindow),
		fyne.NewMenuItem("Закрыть", a.quit),
	)

	// SetSystemTrayIcon здесь не вызываем: трей ещё не запущен и вернёт
	// «tray not ready yet». При инициализации он возьмёт иконку приложения,
	// а дальше её меняет refreshTray.
	desk.SetSystemTrayMenu(a.tray.menu)

	// Левый клик по иконке показывает окно, правый — открывает меню.
	desk.SetSystemTrayWindow(a.win)
}

// refreshTray приводит иконку в трее в соответствие с состоянием.
//
// Меню здесь намеренно не трогается. Обновить один пункт fyne не умеет:
// SetSystemTrayMenu целиком сбрасывает меню через systray.ResetMenu, заново
// создаёт все пункты и на каждый заводит горутину-слушателя. Делать это из
// главного потока на каждый старт и стоп — напрашиваться на неприятности:
// удаление и пересоздание идут вперемешку с показом всплывающего меню
// (TrackPopupMenu держит поток systray, пока меню открыто), а доставка нажатия
// в systray — неблокирующая отправка в канал, и нажатие, пришедшее в момент
// пересборки, просто теряется.
//
// Поэтому пункты «Запустить» и «Остановить» всегда доступны: обе команды
// планировщика идемпотентны, повторное нажатие ничего не портит, а состояние
// видно по цвету иконки и в окне.
func (a *App) refreshTray(st jiggler.State) {
	if a.tray.desk == nil {
		return
	}
	if a.tray.shownOnce && a.tray.shownRunning == st.Running {
		return
	}

	icon := a.iconIdle
	if st.Running {
		icon = a.iconActive
	}
	a.tray.desk.SetSystemTrayIcon(icon)

	a.tray.shownRunning = st.Running
	a.tray.shownOnce = true
}

// rebuildTrayMenu пересобирает меню — единственное, чем можно обновить галочку
// автозапуска. Вызывается только по явному действию пользователя и с задержкой:
// к моменту пересборки всплывающее меню, из которого пришло нажатие, должно
// успеть закрыться.
func (a *App) rebuildTrayMenu() {
	if a.tray.desk == nil {
		return
	}
	a.tray.itemAutorun.Checked = a.autorunOn

	time.AfterFunc(trayRebuildDelay, func() {
		fyne.Do(func() {
			if a.shuttingDown.Load() {
				return
			}
			a.tray.desk.SetSystemTrayMenu(a.tray.menu)
		})
	})
}
