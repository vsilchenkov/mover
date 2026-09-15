//go:build windows

package mouse

import (
	"testing"
	"unsafe"
)

// TestInputLayout стережёт раскладку структур, передаваемых в SendInput:
// лишний или недостающий байт превращает вызов в тихо игнорируемый.
func TestInputLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("проверка рассчитана на 64-битную сборку")
	}

	if got := unsafe.Sizeof(mouseInput{}); got != 32 {
		t.Errorf("sizeof(MOUSEINPUT) = %d, ожидалось 32", got)
	}
	if got := unsafe.Sizeof(input{}); got != 40 {
		t.Errorf("sizeof(INPUT) = %d, ожидалось 40", got)
	}
	if got := unsafe.Offsetof(input{}.mi); got != 8 {
		t.Errorf("смещение MOUSEINPUT внутри INPUT = %d, ожидалось 8", got)
	}
	if got := unsafe.Sizeof(lastInputInfo{}); got != 8 {
		t.Errorf("sizeof(LASTINPUTINFO) = %d, ожидалось 8", got)
	}
}

// TestAbsDiffWraparound проверяет, что разность отметок GetTickCount остаётся
// осмысленной при переполнении счётчика (примерно раз в 49,7 суток аптайма).
func TestAbsDiffWraparound(t *testing.T) {
	tests := []struct {
		name string
		a, b uint32
		want uint32
	}{
		{"обычная разность", 5000, 4200, 800},
		{"обратный порядок", 4200, 5000, 800},
		{"совпадение", 777, 777, 0},
		{"через переполнение", 100, ^uint32(0) - 99, 200},
		{"через переполнение наоборот", ^uint32(0) - 99, 100, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := absDiff(tt.a, tt.b); got != tt.want {
				t.Errorf("absDiff(%d, %d) = %d, ожидалось %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name         string
		offset, size int32
		want         int32
	}{
		{"левый край", 0, 1920, 0},
		{"правый край", 1919, 1920, absoluteRange},
		{"середина", 960, 1921, absoluteRange / 2},
		{"вырожденный размер", 10, 1, 0},
		{"отрицательное смещение обрезается", -50, 1920, 0},
		{"выход за край обрезается", 5000, 1920, absoluteRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalize(tt.offset, tt.size); got != tt.want {
				t.Errorf("normalize(%d, %d) = %d, ожидалось %d", tt.offset, tt.size, got, tt.want)
			}
		})
	}
}

// TestIdleMillisReal дёргает реальный GetLastInputInfo: он должен отвечать
// без ошибки и давать правдоподобное время простоя.
func TestIdleMillisReal(t *testing.T) {
	m := New()
	idle, ours := m.IdleMillis()

	if ours {
		t.Error("до единого вызова Jiggle ввод не может считаться нашим")
	}
	// Неделя простоя означала бы, что мы читаем мусор.
	const week = uint32(7 * 24 * 3600 * 1000)
	if idle > week {
		t.Errorf("неправдоподобное время простоя: %d мс", idle)
	}
}
