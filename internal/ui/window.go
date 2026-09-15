package ui

import (
	"fmt"
	"image/color"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mover/internal/config"
	"mover/internal/jiggler"
	"mover/internal/logging"
	"mover/internal/mouse"
)

// statusDotSize — диаметр индикатора состояния.
const statusDotSize = 14

// widgets — виджеты окна, которые приходится обновлять по ходу работы.
type widgets struct {
	statusDot  *canvas.Circle
	statusText *valueText
	nextText   *valueText
	progress   *widget.ProgressBar

	btnStart *widget.Button
	btnStop  *widget.Button

	tileCount  *valueText
	tileUptime *valueText
	tilePixels *valueText

	entryMin *widget.Entry
	entryMax *widget.Entry
	entryPx  *widget.Entry

	checkSkip  *widget.Check
	checkAwake *widget.Check

	pulse *fyne.Animation
}

func (a *App) buildWindow() {
	a.win = a.fyneApp.NewWindow(windowTitle)
	a.win.SetIcon(a.iconActive)
	a.win.SetContent(a.buildContent())
	a.win.Resize(fyne.NewSize(windowWidth, windowHeight))
	a.win.SetFixedSize(true)

	// CenterOnScreen намеренно не вызывается: внутри fyne это приводит к
	// glfw.Monitor.GetVideoMode, а тот разыменовывает nil, если список
	// мониторов пуст — например, когда сеанс заблокирован или отключён.
	// Приложение из автозагрузки вполне может стартовать в такой момент,
	// и падать из-за расположения окна оно не должно. Позицию выберет
	// сама Windows.

	// Крестик прячет окно в трей, а не завершает приложение: выйти можно
	// только через пункт меню «Закрыть».
	a.win.SetCloseIntercept(a.hideToTray)

	a.applyState(jiggler.State{})
}

func (a *App) buildContent() fyne.CanvasObject {
	body := container.NewVBox(
		a.buildStatusCard(),
		a.buildButtons(),
		a.buildTiles(),
		widget.NewSeparator(),
		a.buildSettings(),
	)
	return container.NewBorder(a.buildHeader(), nil, nil, nil, container.NewPadded(body))
}

// buildHeader рисует шапку: градиентная полоса с названием.
//
// Цвета здесь фиксированные, а не из палитры: акцентный индиго одинаково
// уместен и на светлом, и на тёмном оформлении, а белый текст читается на нём
// в обоих случаях.
func (a *App) buildHeader() fyne.CanvasObject {
	grad := canvas.NewHorizontalGradient(
		color.NRGBA{0x4F, 0x46, 0xE5, 0xFF},
		color.NRGBA{0x7C, 0x7A, 0xFF, 0xFF},
	)

	title := canvas.NewText("MOVER", color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}

	subtitle := canvas.NewText("антиблокировка экрана", color.NRGBA{0xFF, 0xFF, 0xFF, 0xCC})
	subtitle.TextSize = 12

	text := container.NewPadded(container.NewVBox(title, subtitle))
	return container.NewStack(grad, text)
}

// buildStatusCard — карточка состояния: индикатор, надпись, обратный отсчёт
// и полоса прогресса до следующего движения.
func (a *App) buildStatusCard() fyne.CanvasObject {
	a.widgets.statusDot = canvas.NewCircle(currentPalette().muted)
	dot := container.NewCenter(
		container.New(layout.NewGridWrapLayout(fyne.NewSize(statusDotSize, statusDotSize)),
			a.widgets.statusDot),
	)

	a.widgets.statusText = newValueText("Остановлен", theme.SizeNameSubHeadingText, true, fyne.TextAlignLeading)
	a.widgets.nextText = newValueText("—", theme.SizeNameText, false, fyne.TextAlignTrailing)

	a.widgets.progress = widget.NewProgressBar()
	a.widgets.progress.Min = 0
	a.widgets.progress.Max = 1
	// Подпись с процентами здесь только мешает: важна сама полоса.
	a.widgets.progress.TextFormatter = func() string { return "" }

	row := container.NewBorder(nil, nil, dot, a.widgets.nextText, a.widgets.statusText)
	return widget.NewCard("", "", container.NewVBox(row, a.widgets.progress))
}

func (a *App) buildButtons() fyne.CanvasObject {
	a.widgets.btnStart = widget.NewButtonWithIcon("СТАРТ", theme.MediaPlayIcon(), a.start)
	a.widgets.btnStart.Importance = widget.SuccessImportance

	a.widgets.btnStop = widget.NewButtonWithIcon("СТОП", theme.MediaStopIcon(), a.stop)
	a.widgets.btnStop.Importance = widget.DangerImportance

	return container.NewGridWithColumns(2, a.widgets.btnStart, a.widgets.btnStop)
}

// buildTiles — три плитки со статистикой.
func (a *App) buildTiles() fyne.CanvasObject {
	a.widgets.tileCount = newValueText("0", theme.SizeNameSubHeadingText, true, fyne.TextAlignCenter)
	a.widgets.tileUptime = newValueText("00:00", theme.SizeNameSubHeadingText, true, fyne.TextAlignCenter)
	a.widgets.tilePixels = newValueText("0 px", theme.SizeNameSubHeadingText, true, fyne.TextAlignCenter)

	return container.NewGridWithColumns(3,
		tileCard(a.widgets.tileCount, "движений"),
		tileCard(a.widgets.tileUptime, "в работе"),
		tileCard(a.widgets.tilePixels, "сдвиг"),
	)
}

func tileCard(value *valueText, caption string) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(caption, fyne.TextAlignCenter, fyne.TextStyle{})
	label.Importance = widget.LowImportance
	return widget.NewCard("", "", container.NewVBox(value, label))
}

// buildSettings — поля интервала и сдвига плюс переключатели.
func (a *App) buildSettings() fyne.CanvasObject {
	a.widgets.entryMin = a.numberEntry(a.cfg.IntervalMinSec, config.MinIntervalSec, config.MaxIntervalSec)
	a.widgets.entryMax = a.numberEntry(a.cfg.IntervalMaxSec, config.MinIntervalSec, config.MaxIntervalSec)
	a.widgets.entryPx = a.numberEntry(a.cfg.MovePixels, config.MinPixels, config.MaxPixels)

	interval := container.NewGridWithColumns(2, a.widgets.entryMin, a.widgets.entryMax)

	form := container.New(layout.NewFormLayout(),
		widget.NewLabel("Интервал, с"), interval,
		widget.NewLabel("Сдвиг, px"), a.widgets.entryPx,
	)

	a.widgets.checkSkip = widget.NewCheck("Не мешать, когда я работаю", func(bool) { a.onSettingChanged() })
	a.widgets.checkAwake = widget.NewCheck("Держать экран включённым", func(bool) { a.onSettingChanged() })

	a.withoutEcho(func() {
		a.widgets.checkSkip.SetChecked(a.cfg.SkipWhenUserActive)
		a.widgets.checkAwake.SetChecked(a.cfg.KeepScreenAwake)
	})

	return container.NewVBox(form, a.widgets.checkSkip, a.widgets.checkAwake)
}

// numberEntry создаёт поле для целого числа с проверкой границ.
func (a *App) numberEntry(value, min, max int) *widget.Entry {
	e := widget.NewEntry()
	e.Validator = func(s string) error {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("нужно целое число")
		}
		if v < min || v > max {
			return fmt.Errorf("от %d до %d", min, max)
		}
		return nil
	}
	e.OnChanged = func(string) { a.onSettingChanged() }

	a.withoutEcho(func() { e.SetText(strconv.Itoa(value)) })
	return e
}

// ----- реакция на изменения --------------------------------------------------

// onSettingChanged собирает настройки из виджетов, применяет их к планировщику
// и сохраняет в файл. Некорректные поля просто игнорируются — пользователь
// видит подсветку ошибки и продолжает набор.
func (a *App) onSettingChanged() {
	if a.applying {
		return
	}

	cfg := a.cfg
	if v, ok := entryValue(a.widgets.entryMin); ok {
		cfg.IntervalMinSec = v
	}
	if v, ok := entryValue(a.widgets.entryMax); ok {
		cfg.IntervalMaxSec = v
	}
	if v, ok := entryValue(a.widgets.entryPx); ok {
		cfg.MovePixels = v
	}
	cfg.SkipWhenUserActive = a.widgets.checkSkip.Checked
	cfg.KeepScreenAwake = a.widgets.checkAwake.Checked
	cfg.Normalize()

	if cfg == a.cfg {
		return
	}
	a.cfg = cfg

	if a.jig != nil {
		a.jig.Reconfigure(cfg.Jiggler())
		mouse.KeepAwake(cfg.KeepScreenAwake && a.jig.State().Running)
	}
	a.updateTiles(a.jigState())

	if err := config.Save(cfg); err != nil {
		slog.Error("не удалось сохранить настройки", logging.Err(err))
	}
}

// entryValue возвращает число из поля, если оно прошло проверку.
func entryValue(e *widget.Entry) (int, bool) {
	if e.Validate() != nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(e.Text))
	if err != nil {
		return 0, false
	}
	return v, true
}

// withoutEcho выполняет действие, не считая вызванные им события действиями
// пользователя: SetText и SetChecked дёргают те же обработчики.
func (a *App) withoutEcho(fn func()) {
	a.applying = true
	defer func() { a.applying = false }()
	fn()
}

func (a *App) jigState() jiggler.State {
	if a.jig == nil {
		return jiggler.State{}
	}
	return a.jig.State()
}

// ----- обновление виджетов ---------------------------------------------------

func (a *App) updateStatus(st jiggler.State) {
	p := currentPalette()

	if st.Running {
		a.widgets.statusText.set("Активен")
		a.widgets.statusDot.FillColor = p.success
		a.widgets.btnStart.Disable()
		a.widgets.btnStop.Enable()
	} else {
		a.widgets.statusText.set("Остановлен")
		a.widgets.statusDot.FillColor = p.muted
		a.widgets.btnStart.Enable()
		a.widgets.btnStop.Disable()
	}
	canvas.Refresh(a.widgets.statusDot)

	a.setPulse(st.Running)
	a.updateCountdown(st)
}

// updateCountdown обновляет время до следующего движения и полосу прогресса.
func (a *App) updateCountdown(st jiggler.State) {
	if !st.Running || st.NextAt.IsZero() {
		a.widgets.nextText.set("—")
		a.widgets.progress.SetValue(0)
		return
	}

	remaining := time.Until(st.NextAt)
	a.widgets.nextText.set("след. через " + formatClock(remaining))

	if st.Interval > 0 {
		done := float64(st.Interval-remaining) / float64(st.Interval)
		a.widgets.progress.SetValue(clampFloat(done, 0, 1))
	}
}

func (a *App) updateTiles(st jiggler.State) {
	a.widgets.tileCount.set(strconv.Itoa(st.Count))

	uptime := "00:00"
	if st.Running && !st.StartedAt.IsZero() {
		uptime = formatClock(time.Since(st.StartedAt))
	}
	a.widgets.tileUptime.set(uptime)

	a.widgets.tilePixels.set(fmt.Sprintf("%d px", a.cfg.MovePixels))
}

// setPulse включает мягкую пульсацию индикатора, пока планировщик работает.
func (a *App) setPulse(on bool) {
	if a.widgets.pulse != nil {
		a.widgets.pulse.Stop()
		a.widgets.pulse = nil
	}
	if !on {
		return
	}

	p := currentPalette()
	anim := canvas.NewColorRGBAAnimation(p.success, withAlpha(p.success, 0x55),
		900*time.Millisecond, func(c color.Color) {
			a.widgets.statusDot.FillColor = c
			canvas.Refresh(a.widgets.statusDot)
		})
	anim.RepeatCount = fyne.AnimationRepeatForever
	anim.AutoReverse = true
	anim.Start()

	a.widgets.pulse = anim
}

func clampFloat(v, lo, hi float64) float64 {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// ----- текст с управляемым размером ------------------------------------------

// valueText — текст, у которого нужно менять содержимое, сохраняя стиль.
//
// canvas.Text не подошёл бы: у него фиксированный цвет, который не следует
// за сменой светлой и тёмной темы Windows. RichText берёт цвет из темы сам.
type valueText struct {
	*widget.RichText
	seg *widget.TextSegment
}

func newValueText(text string, size fyne.ThemeSizeName, bold bool, align fyne.TextAlign) *valueText {
	seg := &widget.TextSegment{
		Text: text,
		Style: widget.RichTextStyle{
			SizeName:  size,
			TextStyle: fyne.TextStyle{Bold: bold},
			Alignment: align,
			Inline:    true,
		},
	}
	return &valueText{RichText: widget.NewRichText(seg), seg: seg}
}

func (v *valueText) set(text string) {
	if v.seg.Text == text {
		return
	}
	v.seg.Text = text
	v.Refresh()
}
