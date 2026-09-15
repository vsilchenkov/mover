package ui

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"mover/internal/config"
	"mover/internal/jiggler"
)

// forcedVariant прибивает вариант оформления гвоздями: тестовое приложение
// fyne всегда считает себя тёмным, а посмотреть надо на оба вида.
type forcedVariant struct{ v fyne.ThemeVariant }

func (f forcedVariant) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return moverTheme{}.Color(n, f.v)
}
func (f forcedVariant) Font(s fyne.TextStyle) fyne.Resource     { return moverTheme{}.Font(s) }
func (f forcedVariant) Icon(n fyne.ThemeIconName) fyne.Resource { return moverTheme{}.Icon(n) }
func (f forcedVariant) Size(n fyne.ThemeSizeName) float32       { return moverTheme{}.Size(n) }

// TestRenderVariants сохраняет вёрстку в четырёх сочетаниях «тема × состояние».
// Тест не сравнивает картинки с эталоном — он даёт их посмотреть глазами,
// потому что снять скриншот работающего окна из консоли нельзя: OpenGL-поверхность
// в GDI-захват не попадает.
func TestRenderVariants(t *testing.T) {
	cases := []struct {
		name    string
		variant fyne.ThemeVariant
		running bool
	}{
		{"dark-stopped", theme.VariantDark, false},
		{"dark-running", theme.VariantDark, true},
		{"light-stopped", theme.VariantLight, false},
		{"light-running", theme.VariantLight, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fyneApp := test.NewApp()
			t.Cleanup(func() { test.NewApp() })
			fyneApp.Settings().SetTheme(forcedVariant{v: tc.variant})

			a := &App{
				fyneApp: fyneApp,
				cfg:     config.Default(),
				done:    make(chan struct{}),
			}

			w := test.NewWindow(a.buildContent())
			w.Resize(fyne.NewSize(windowWidth, windowHeight))
			t.Cleanup(w.Close)

			st := jiggler.State{}
			if tc.running {
				st = jiggler.State{
					Running:   true,
					NextAt:    time.Now().Add(38 * time.Second),
					Interval:  60 * time.Second,
					Count:     128,
					StartedAt: time.Now().Add(-83 * time.Second),
				}
			}
			a.updateStatus(st)
			t.Cleanup(func() { a.setPulse(false) })
			a.updateTiles(st)

			img := w.Canvas().Capture()
			if img == nil {
				t.Fatal("холст не отрисовался")
			}

			out := filepath.Join(os.TempDir(), "mover-"+tc.name+".png")
			f, err := os.Create(out)
			if err != nil {
				t.Fatalf("создание %s: %v", out, err)
			}
			defer f.Close()
			if err := png.Encode(f, img); err != nil {
				t.Fatalf("кодирование: %v", err)
			}
			t.Logf("сохранено: %s", out)
		})
	}
}
