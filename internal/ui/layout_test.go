package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"mover/internal/config"
	"mover/internal/jiggler"
)

// newTestApp собирает интерфейс на программном рендерере fyne: настоящий
// OpenGL и экран для этого не нужны.
func newTestApp(t *testing.T) (*App, fyne.Window) {
	t.Helper()

	fyneApp := test.NewApp()
	t.Cleanup(func() { test.NewApp() }) // вернуть окружение в исходное состояние
	fyneApp.Settings().SetTheme(moverTheme{})

	a := &App{
		fyneApp: fyneApp,
		cfg:     config.Default(),
		done:    make(chan struct{}),
	}

	w := test.NewWindow(a.buildContent())
	w.Resize(fyne.NewSize(windowWidth, windowHeight))
	t.Cleanup(w.Close)

	return a, w
}

// TestLayoutRenders проверяет, что окно вообще собирается и рисуется, а заодно
// сохраняет картинку — по ней удобно смотреть на вёрстку без запуска exe.
func TestLayoutRenders(t *testing.T) {
	a, w := newTestApp(t)

	img := w.Canvas().Capture()
	if img == nil {
		t.Fatal("холст не отрисовался")
	}
	b := img.Bounds()
	if b.Dx() < windowWidth || b.Dy() < windowHeight {
		t.Errorf("размер холста %dx%d меньше окна %vx%v", b.Dx(), b.Dy(), windowWidth, windowHeight)
	}

	// Картинка нужна для глазами-проверки вёрстки, поэтому пишется всегда,
	// а не только при провале.
	out := filepath.Join(os.TempDir(), "mover-layout.png")
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("создание %s: %v", out, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("кодирование картинки: %v", err)
	}
	t.Logf("вёрстка сохранена: %s", out)

	if a.widgets.statusText == nil || a.widgets.btnStart == nil {
		t.Fatal("виджеты не собраны")
	}
}

// TestInitialStateStopped проверяет исходное состояние: таймер стоит,
// доступна только кнопка запуска.
func TestInitialStateStopped(t *testing.T) {
	a, _ := newTestApp(t)
	a.applyState(jiggler.State{})

	if a.widgets.btnStart.Disabled() {
		t.Error("в покое кнопка «СТАРТ» должна быть доступна")
	}
	if !a.widgets.btnStop.Disabled() {
		t.Error("в покое кнопка «СТОП» должна быть недоступна")
	}
}

// TestSettingsEntriesValidate проверяет, что поля отбраковывают значения
// вне допустимых границ.
func TestSettingsEntriesValidate(t *testing.T) {
	a, _ := newTestApp(t)

	tests := []struct {
		name  string
		text  string
		valid bool
	}{
		{"нормальное значение", "45", true},
		{"нижняя граница", "5", true},
		{"верхняя граница", "3600", true},
		{"меньше минимума", "1", false},
		{"больше максимума", "99999", false},
		{"не число", "абв", false},
		{"пусто", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.widgets.entryMin.SetText(tt.text)
			err := a.widgets.entryMin.Validate()
			if tt.valid && err != nil {
				t.Errorf("значение %q забраковано: %v", tt.text, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("значение %q принято, хотя не должно", tt.text)
			}
		})
	}
}

// TestInvalidInputKeepsConfig — набор заведомо неверного значения не должен
// портить рабочие настройки.
func TestInvalidInputKeepsConfig(t *testing.T) {
	a, _ := newTestApp(t)
	before := a.cfg

	a.widgets.entryMin.SetText("0")
	a.onSettingChanged()

	if a.cfg.IntervalMinSec != before.IntervalMinSec {
		t.Errorf("настройки испортились: было %d, стало %d",
			before.IntervalMinSec, a.cfg.IntervalMinSec)
	}
}
