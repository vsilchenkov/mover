// Пакет ui собирает интерфейс приложения на fyne: окно с управлением,
// иконку в трее и связку с планировщиком.
//
// Планировщик живёт в своей горутине, поэтому любое обновление виджетов
// из его колбэка обёрнуто в fyne.Do — начиная с fyne 2.6 прямые вызовы
// из чужой горутины запрещены.
package ui

import (
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"mover/assets"
	"mover/internal/autorun"
	"mover/internal/config"
	"mover/internal/instance"
	"mover/internal/jiggler"
	"mover/internal/logging"
	"mover/internal/mouse"
)

const (
	appID       = "dev.svv.mover"
	windowTitle = "Mover"

	windowWidth  = 440
	windowHeight = 570

	// countdownPeriod — как часто обновляется обратный отсчёт до следующего движения.
	countdownPeriod = time.Second
)

// App — состояние интерфейса и точка связи всех частей приложения.
type App struct {
	fyneApp fyne.App
	win     fyne.Window
	jig     *jiggler.Jiggler
	inst    *instance.Instance

	cfg config.Config

	widgets widgets
	tray    tray

	iconActive fyne.Resource
	iconIdle   fyne.Resource

	// autorunOn отражает состояние записи в реестре автозагрузки.
	autorunOn bool

	// applying защищает от эха: при программной установке значений виджетов
	// их обработчики не должны считать это действием пользователя.
	applying bool

	// windowVisible — показано ли окно. Своего Visible() у fyne.Window нет,
	// а фоновым проверкам это состояние нужно.
	windowVisible atomic.Bool
	// hiddenByLock отмечает, что окно убрали мы сами на время блокировки
	// сеанса, — значит, после разблокировки его нужно вернуть.
	hiddenByLock atomic.Bool

	// shuttingDown отключает обновление виджетов на время завершения работы.
	// При остановке fyne переводит очередь задач в режим слива, и тогда
	// fyne.Do выполняет функцию прямо на вызывающей горутине, а не на главной.
	// Последний publish планировщика в этот момент трогал бы виджеты из чужого
	// потока — fyne на такое ругается и обещает в следующей мажорной версии
	// снять страховку.
	shuttingDown atomic.Bool

	done chan struct{}
}

// Run строит интерфейс и передаёт управление циклу событий fyne.
// Возвращается только при завершении приложения.
func Run(cfg config.Config, flags config.Flags, inst *instance.Instance) {
	a := &App{
		fyneApp: app.NewWithID(appID),
		cfg:     cfg,
		inst:    inst,
		done:    make(chan struct{}),
	}
	a.fyneApp.Settings().SetTheme(moverTheme{})

	a.iconActive = fyne.NewStaticResource("icon-active.png", assets.IconActive)
	a.iconIdle = fyne.NewStaticResource("icon-idle.png", assets.IconIdle)

	// Иконку приложения ставим «спящую»: трей стартует позже окна и при
	// инициализации берёт именно её. Задать иконку трея заранее нельзя —
	// fyne ответит «tray not ready yet». Окну своя иконка назначается
	// отдельно, в buildWindow.
	a.fyneApp.SetIcon(a.iconIdle)

	// Состояние автозапуска читаем из реестра до сборки меню: галочка в трее
	// должна показывать реальность, а не наши предположения.
	if on, err := autorun.IsEnabled(); err != nil {
		slog.Warn("не удалось прочитать состояние автозапуска", logging.Err(err))
	} else {
		a.autorunOn = on
	}

	a.buildWindow()
	a.buildTray()
	a.watchWindowState()

	if inst != nil {
		inst.WatchShowRequests(func() { fyne.Do(a.showWindow) })
	}

	a.jig = jiggler.New(mouse.New(), cfg.Jiggler(), func(st jiggler.State) {
		if a.shuttingDown.Load() {
			return
		}
		fyne.Do(func() { a.applyState(st) })
	})
	defer func() {
		a.shuttingDown.Store(true)
		a.jig.Close()
	}()

	go a.countdownLoop()

	// Включённый автозапуск означает и автостарт таймера: пользователь
	// один раз сказал «работай сама», повторных нажатий не требуется.
	if a.autorunOn {
		slog.Info("автозапуск включён — стартуем таймер")
		a.jig.Start()
	}

	if flags.Autostart {
		slog.Info("запуск из автозагрузки: окно не показываем")
	} else {
		a.win.Show()
		a.windowVisible.Store(true)
	}

	a.fyneApp.Run()
}

// ----- действия --------------------------------------------------------------

func (a *App) start() { a.jig.Start() }

func (a *App) stop() { a.jig.Stop() }

// showWindow достаёт окно из трея и выводит на передний план.
func (a *App) showWindow() {
	a.win.Show()
	a.windowVisible.Store(true)
	a.win.RequestFocus()
	a.restoreFromMinimized()
}

// hideToTray прячет окно, не завершая приложение.
//
// Именно Hide, а не Close: fyne завершает приложение, когда закрыто последнее
// окно, а скрытое окно остаётся в списке живых. Побочный эффект скрытия важен
// сам по себе — fyne перестаёт перерисовывать окно, а значит, прекращает
// работать с OpenGL.
func (a *App) hideToTray() {
	a.win.Hide()
	a.windowVisible.Store(false)
}

// quit завершает приложение.
func (a *App) quit() {
	slog.Info("завершение по команде пользователя")

	// Порядок важен: сначала отключаем обновление виджетов, и только потом
	// останавливаем планировщик — его прощальный publish не должен добраться
	// до интерфейса, который уже сворачивается.
	a.shuttingDown.Store(true)

	select {
	case <-a.done:
	default:
		close(a.done)
	}
	mouse.KeepAwake(false)
	a.jig.Stop()
	if a.inst != nil {
		a.inst.Release()
	}
	a.fyneApp.Quit()
}

// toggleAutorun переключает автозапуск с Windows.
//
// Он же управляет автостартом таймера: отдельной настройки нет, состояние
// хранится только в реестре.
func (a *App) toggleAutorun() {
	want := !a.autorunOn

	if err := autorun.Set(want); err != nil {
		// Записать в реестр может помешать политика безопасности. Молча
		// притворяться, что получилось, нельзя: галочка врала бы пользователю.
		slog.Error("не удалось изменить автозапуск", logging.Err(err))
		a.notify("Автозапуск", fmt.Sprintf("Не удалось изменить: %v", err))
		return
	}

	a.autorunOn = want
	slog.Info("автозапуск изменён", slog.Bool("включён", want))
	a.rebuildTrayMenu()
}

// ----- обновление интерфейса -------------------------------------------------

// applyState разносит состояние планировщика по виджетам. Вызывается только
// в основном потоке fyne.
func (a *App) applyState(st jiggler.State) {
	a.updateStatus(st)
	a.updateTiles(st)
	a.refreshTray(st)

	if a.cfg.KeepScreenAwake {
		mouse.KeepAwake(st.Running)
	} else {
		mouse.KeepAwake(false)
	}
}

// countdownLoop раз в секунду обновляет обратный отсчёт до следующего движения.
func (a *App) countdownLoop() {
	ticker := time.NewTicker(countdownPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			// Пока окно скрыто, обновлять нечего. Дело не только в экономии:
			// каждое изменение виджета помечает холст «грязным» и заставляет
			// fyne рисовать кадр, а рисовать в невидимое окно незачем.
			if !a.windowVisible.Load() {
				continue
			}
			fyne.Do(func() {
				st := a.jig.State()
				a.updateCountdown(st)
				a.updateTiles(st)
			})
		}
	}
}

// notify показывает всплывающее сообщение через штатные уведомления системы.
func (a *App) notify(title, body string) {
	a.fyneApp.SendNotification(fyne.NewNotification(title, body))
}

// ----- форматирование --------------------------------------------------------

// formatClock переводит длительность в ММ:СС, а от часа — в Ч:ММ:СС.
func formatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
