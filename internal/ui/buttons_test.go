package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"mover/internal/config"
	"mover/internal/jiggler"
)

// stubMover изображает мышь, ничего не двигая: тесту интерфейса важна только
// реакция виджетов, а не Windows API.
type stubMover struct{}

func (stubMover) Jiggle(int) error           { return nil }
func (stubMover) IdleMillis() (uint32, bool) { return 60_000, false }

// withJiggler добавляет к тестовому приложению настоящий планировщик,
// связанный с виджетами так же, как в бою.
func withJiggler(t *testing.T, a *App) {
	t.Helper()

	a.jig = jiggler.New(stubMover{}, config.Default().Jiggler(), func(st jiggler.State) {
		a.applyState(st)
	})
	t.Cleanup(a.jig.Close)
}

// TestStartButtonSwitchesState нажимает «СТАРТ» механикой самой fyne и
// проверяет, что состояние действительно переключилось. Тест отделяет нашу
// половину задачи от чужой: если он проходит, а в живом приложении кнопка
// не отзывается, дело не в связке виджетов и планировщика.
func TestStartButtonSwitchesState(t *testing.T) {
	a, _ := newTestApp(t)
	withJiggler(t, a)

	if a.widgets.btnStart.Disabled() {
		t.Fatal("до нажатия «СТАРТ» должна быть доступна")
	}

	test.Tap(a.widgets.btnStart)

	if st := a.jig.State(); !st.Running {
		t.Fatal("планировщик не запустился по нажатию кнопки")
	}
	if !a.widgets.btnStart.Disabled() {
		t.Error("после запуска «СТАРТ» должна погаснуть")
	}
	if a.widgets.btnStop.Disabled() {
		t.Error("после запуска «СТОП» должна стать доступной")
	}
	if got := a.widgets.statusText.seg.Text; got != "Активен" {
		t.Errorf("статус = %q, ожидалось «Активен»", got)
	}
}

// TestStopButtonSwitchesState — то же для остановки.
func TestStopButtonSwitchesState(t *testing.T) {
	a, _ := newTestApp(t)
	withJiggler(t, a)

	test.Tap(a.widgets.btnStart)
	test.Tap(a.widgets.btnStop)

	if st := a.jig.State(); st.Running {
		t.Fatal("планировщик не остановился по нажатию кнопки")
	}
	if a.widgets.btnStart.Disabled() {
		t.Error("после остановки «СТАРТ» должна снова стать доступной")
	}
	if !a.widgets.btnStop.Disabled() {
		t.Error("после остановки «СТОП» должна погаснуть")
	}
	if got := a.widgets.statusText.seg.Text; got != "Остановлен" {
		t.Errorf("статус = %q, ожидалось «Остановлен»", got)
	}
}

// TestRepeatedTapsStaySane — повторные нажатия не должны ломать состояние:
// обе команды планировщика идемпотентны.
func TestRepeatedTapsStaySane(t *testing.T) {
	a, _ := newTestApp(t)
	withJiggler(t, a)

	for range 5 {
		test.Tap(a.widgets.btnStart)
		test.Tap(a.widgets.btnStop)
	}

	if st := a.jig.State(); st.Running {
		t.Error("после последнего «СТОП» планировщик должен стоять")
	}
	if a.widgets.btnStart.Disabled() || !a.widgets.btnStop.Disabled() {
		t.Error("кнопки разъехались с состоянием после серии нажатий")
	}
}

// TestTapDoesNotBlock стережёт отзывчивость: нажатие обращается к планировщику
// синхронно, и если тот занят тиком, интерфейс ждёт вместе с ним.
func TestTapDoesNotBlock(t *testing.T) {
	a, _ := newTestApp(t)
	withJiggler(t, a)

	done := make(chan struct{})
	go func() {
		test.Tap(a.widgets.btnStart)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("нажатие кнопки заблокировалось дольше двух секунд")
	}
}
