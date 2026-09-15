//go:build windows

// Пакет mouse — тонкая обёртка над user32/kernel32: микро-движение курсора,
// определение простоя пользователя и запрет на гашение экрана.
//
// Курсор двигается только через SendInput. SetCursorPos не подходит
// принципиально: он перемещает указатель, но не порождает события ввода,
// поэтому таймер простоя Windows не сбрасывается и экран всё равно блокируется.
package mouse

import (
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSendInput        = user32.NewProc("SendInput")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")

	procGetTickCount            = kernel32.NewProc("GetTickCount")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)

const (
	inputMouse = 0

	mouseEventFMove        = 0x0001
	mouseEventFAbsolute    = 0x8000
	mouseEventFVirtualDesk = 0x4000

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
	esContinuous      = 0x80000000

	// absoluteRange — диапазон нормированных координат SendInput.
	absoluteRange = 65535

	// settleDelay — пауза между сдвигом и возвратом курсора. Нужна, чтобы
	// система восприняла это как два отдельных перемещения, а не как одно.
	settleDelay = 50 * time.Millisecond

	// injectEpsilon — допуск при сравнении отметки последнего ввода с нашей
	// собственной: между вызовом SendInput и чтением GetTickCount проходит
	// несколько миллисекунд.
	injectEpsilon = 500

	// returnTolerance — на сколько пикселей курсор может отличаться от места,
	// куда мы его поставили, чтобы возврат считался безопасным.
	returnTolerance = 2
)

// mouseInput повторяет MOUSEINPUT из winuser.h. На windows/amd64 занимает
// 32 байта: пять DWORD-ов, 4 байта выравнивания и указатель.
// dwExtraInfo обязан быть uintptr — с uint32/uint64 раскладка поедет.
type mouseInput struct {
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input повторяет INPUT из winuser.h: тип, явное выравнивание и объединение,
// от которого нам нужен только вариант с мышью. На windows/amd64 — 40 байт.
type input struct {
	typ uint32
	_   [4]byte
	mi  mouseInput
}

type point struct {
	x, y int32
}

// lastInputInfo повторяет LASTINPUTINFO из winuser.h.
type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// Mover двигает курсор и отвечает на вопрос, простаивает ли система.
// Все методы безопасны для вызова из одной горутины планировщика;
// отметка последнего инжекта хранится атомарно.
type Mover struct {
	lastInject atomic.Uint32
}

// New создаёт Mover.
func New() *Mover { return &Mover{} }

// Jiggle сдвигает курсор на px пикселей и возвращает обратно.
//
// Движение абсолютное: относительный сдвиг проходит через ускорение мыши и
// «повышенную точность установки указателя», из-за чего 1–2 пикселя могут
// округлиться в ноль и курсор фактически не сдвинется.
func (m *Mover) Jiggle(px int) error {
	if px <= 0 {
		px = 1
	}

	start, err := cursorPos()
	if err != nil {
		return err
	}

	vx, _, vw, _, err := virtualScreen()
	if err != nil {
		return err
	}

	// У правого края экрана сдвигаемся влево, иначе Windows обрежет движение.
	delta := int32(px)
	if start.x+delta >= vx+vw-1 {
		delta = -delta
	}

	if err := moveAbsolute(point{start.x + delta, start.y}); err != nil {
		return err
	}
	time.Sleep(settleDelay)

	// Возвращаем курсор только если он всё ещё там, куда мы его поставили:
	// если за эти миллисекунды пользователь взялся за мышь, мешать ему нельзя.
	if now, err := cursorPos(); err == nil && near(now, point{start.x + delta, start.y}) {
		if err := moveAbsolute(start); err != nil {
			return err
		}
	}

	m.lastInject.Store(tickCount())
	return nil
}

// IdleMillis возвращает время простоя системы в миллисекундах и признак того,
// что последний зафиксированный ввод — это наш собственный инжект.
//
// Признак необходим: SendInput обновляет GetLastInputInfo так же, как живой
// пользователь, поэтому без него система всегда выглядела бы активной.
func (m *Mover) IdleMillis() (uint32, bool) {
	li := lastInputInfo{}
	li.cbSize = uint32(unsafe.Sizeof(li))

	r1, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	if r1 == 0 {
		slog.Debug("GetLastInputInfo не отработал", slog.Any("error", err))
		return 0, false
	}

	idle := tickCount() - li.dwTime // арифметика uint32 корректна при переполнении

	last := m.lastInject.Load()
	ours := last != 0 && absDiff(li.dwTime, last) <= injectEpsilon

	return idle, ours
}

// KeepAwake включает или снимает запрет на гашение экрана и уход в сон.
//
// Это дополнение к движению курсора, а не замена: таймер простоя оно не
// сбрасывает и от блокировки экрана не спасает.
func KeepAwake(on bool) { awake.set(on) }

// ----- внутренняя кухня ------------------------------------------------------

func moveAbsolute(p point) error {
	vx, vy, vw, vh, err := virtualScreen()
	if err != nil {
		return err
	}

	in := input{
		typ: inputMouse,
		mi: mouseInput{
			dx:      normalize(p.x-vx, vw),
			dy:      normalize(p.y-vy, vh),
			dwFlags: mouseEventFMove | mouseEventFAbsolute | mouseEventFVirtualDesk,
		},
	}

	// cbSize берём только из unsafe.Sizeof: зашитая константа рассыплется
	// при смене архитектуры.
	r1, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if r1 != 1 {
		// LazyProc.Call всегда возвращает не-nil error, поэтому судим по r1.
		return fmt.Errorf("SendInput отклонён (принято событий: %d): %w", r1, err)
	}
	return nil
}

// normalize переводит координату в диапазон 0..65535 виртуального рабочего стола.
func normalize(offset, size int32) int32 {
	if size <= 1 {
		return 0
	}
	v := int64(offset) * absoluteRange / int64(size-1)
	switch {
	case v < 0:
		return 0
	case v > absoluteRange:
		return absoluteRange
	}
	return int32(v)
}

// virtualScreen возвращает левый верхний угол и размер виртуального рабочего
// стола — прямоугольника, объединяющего все мониторы.
func virtualScreen() (x, y, w, h int32, err error) {
	x = systemMetric(smXVirtualScreen)
	y = systemMetric(smYVirtualScreen)
	w = systemMetric(smCXVirtualScreen)
	h = systemMetric(smCYVirtualScreen)
	if w <= 1 || h <= 1 {
		return 0, 0, 0, 0, fmt.Errorf("не удалось определить размер рабочего стола: %dx%d", w, h)
	}
	return x, y, w, h, nil
}

func systemMetric(index int32) int32 {
	r1, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(r1)
}

func cursorPos() (point, error) {
	var p point
	r1, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if r1 == 0 {
		return point{}, fmt.Errorf("GetCursorPos: %w", err)
	}
	return p, nil
}

func tickCount() uint32 {
	r1, _, _ := procGetTickCount.Call()
	return uint32(r1)
}

// absDiff — модуль разности двух отметок времени с учётом переполнения uint32
// (счётчик GetTickCount обнуляется примерно через 49,7 суток аптайма).
func absDiff(a, b uint32) uint32 {
	if d := a - b; d <= 1<<31 {
		return d
	}
	return b - a
}

func near(a, b point) bool {
	dx := a.x - b.x
	dy := a.y - b.y
	return abs32(dx) <= returnTolerance && abs32(dy) <= returnTolerance
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// ----- удержание экрана ------------------------------------------------------

// awake владеет состоянием SetThreadExecutionState.
//
// Функция действует на конкретный поток ОС и сбрасывается при его завершении,
// а горутины Go свободно мигрируют между потоками. Поэтому состоянием владеет
// отдельная горутина, намертво привязанная к своему потоку через LockOSThread.
var awake awakeKeeper

type awakeKeeper struct {
	once sync.Once
	// want — желаемое состояние; горутина всегда применяет самое свежее.
	want atomic.Bool
	// signal лишь будит горутину и потому не блокирует вызывающего.
	// KeepAwake зовут из главного потока fyne, а его останавливать нельзя:
	// встанет весь интерфейс.
	signal chan struct{}
}

func (k *awakeKeeper) set(on bool) {
	k.once.Do(func() {
		k.signal = make(chan struct{}, 1)
		started := make(chan struct{})
		go k.loop(started)
		<-started
	})

	k.want.Store(on)
	select {
	case k.signal <- struct{}{}:
	default: // сигнал уже в очереди — горутина прочитает актуальное значение
	}
}

func (k *awakeKeeper) loop(started chan<- struct{}) {
	// Поток не отпускаем и не завершаем: иначе Windows снимет наш запрос.
	runtime.LockOSThread()
	close(started)

	for range k.signal {
		on := k.want.Load()

		state := uintptr(esContinuous)
		if on {
			state |= esDisplayRequired | esSystemRequired
		}
		if r1, _, err := procSetThreadExecutionState.Call(state); r1 == 0 {
			slog.Warn("SetThreadExecutionState не отработал",
				slog.Bool("on", on), slog.Any("error", err))
		}
	}
}
