//go:build windows

package mouse

import (
	"os"
	"testing"
	"time"
)

// hardwareTestEnv — переменная, включающая тесты, которые реально двигают курсор.
const hardwareTestEnv = "MOVER_HW_TEST"

// TestJiggleResetsIdleTimer — главная проверка всего приложения: движение
// через SendInput должно сбрасывать системный счётчик простоя. Именно на нём
// Windows строит автоблокировку экрана, поэтому без сброса программа бесполезна.
//
// Тест двигает настоящий курсор, поэтому по умолчанию пропускается:
//
//	MOVER_HW_TEST=1 go test ./internal/mouse/ -run Hardware -v
func TestJiggleResetsIdleTimer(t *testing.T) {
	requireHardware(t)

	m := New()

	// Даём счётчику простоя подрасти, чтобы сброс был заметен.
	time.Sleep(2500 * time.Millisecond)

	before, _ := m.IdleMillis()
	if before < 1500 {
		t.Skipf("за компьютером работают (простой %d мс) — измерению верить нельзя", before)
	}

	if err := m.Jiggle(2); err != nil {
		t.Fatalf("Jiggle: %v", err)
	}

	after, ours := m.IdleMillis()
	if after > 500 {
		t.Errorf("простой после движения %d мс, ожидалось около нуля: таймер не сбросился", after)
	}
	if !ours {
		t.Error("движение не опознано как собственное — пропуск тиков будет работать неверно")
	}
	t.Logf("простой до движения %d мс, после — %d мс", before, after)
}

// TestJiggleReturnsCursor проверяет, что курсор возвращается на место:
// иначе за ночь он уполз бы через весь экран.
func TestJiggleReturnsCursor(t *testing.T) {
	requireHardware(t)

	m := New()

	start, err := cursorPos()
	if err != nil {
		t.Fatalf("GetCursorPos: %v", err)
	}

	if err := m.Jiggle(3); err != nil {
		t.Fatalf("Jiggle: %v", err)
	}

	end, err := cursorPos()
	if err != nil {
		t.Fatalf("GetCursorPos: %v", err)
	}

	// Допуск в пару пикселей: перевод в нормированные координаты
	// виртуального рабочего стола и обратно округляется.
	if !near(start, end) {
		t.Errorf("курсор не вернулся: было (%d, %d), стало (%d, %d)",
			start.x, start.y, end.x, end.y)
	}
}

func requireHardware(t *testing.T) {
	t.Helper()
	if os.Getenv(hardwareTestEnv) == "" {
		t.Skipf("тест двигает настоящий курсор; запуск: %s=1 go test ...", hardwareTestEnv)
	}
}
