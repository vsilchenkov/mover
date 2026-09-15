package jiggler

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// ----- дублёры ---------------------------------------------------------------

type fakeMover struct {
	mu sync.Mutex

	jiggles []int  // величины сдвига по вызовам Jiggle
	err     error  // что вернуть из Jiggle
	idle    uint32 // что вернуть из IdleMillis
	ours    bool
}

func (f *fakeMover) Jiggle(px int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jiggles = append(f.jiggles, px)
	return f.err
}

func (f *fakeMover) IdleMillis() (uint32, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.idle, f.ours
}

func (f *fakeMover) calls() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.jiggles...)
}

func (f *fakeMover) setIdle(idle uint32, ours bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.idle, f.ours = idle, ours
}

type fakeTimer struct {
	ch chan time.Time
	d  time.Duration
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }
func (t *fakeTimer) Stop()               {}

type fakeClock struct {
	now     time.Time
	created chan *fakeTimer
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		now:     time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		created: make(chan *fakeTimer, 64),
	}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) NewTimer(d time.Duration) timer {
	t := &fakeTimer{ch: make(chan time.Time, 1), d: d}
	c.created <- t
	return t
}

// nextTimer ждёт, пока планировщик закажет очередной таймер.
func (c *fakeClock) nextTimer(t *testing.T) *fakeTimer {
	t.Helper()
	select {
	case tm := <-c.created:
		return tm
	case <-time.After(2 * time.Second):
		t.Fatal("планировщик не завёл таймер")
		return nil
	}
}

// testConfig — конфигурация с фиксированным интервалом, чтобы тесты не зависели
// от случайности.
func testConfig() Config {
	return Config{
		Min:                time.Minute,
		Max:                time.Minute,
		Pixels:             3,
		IdleThreshold:      30 * time.Second,
		SkipWhenUserActive: false,
	}
}

// ----- тесты -----------------------------------------------------------------

func TestStartIsIdempotent(t *testing.T) {
	clk := newFakeClock()
	j := newWithClock(&fakeMover{}, testConfig(), nil, clk)
	defer j.Close()

	j.Start()
	first := clk.nextTimer(t)
	j.Start() // повторный запуск не должен ничего перепланировать

	if st := j.State(); !st.Running {
		t.Fatal("после Start планировщик должен работать")
	}
	select {
	case <-clk.created:
		t.Error("повторный Start завёл лишний таймер")
	default:
	}
	if first == nil {
		t.Fatal("таймер не создан")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	clk := newFakeClock()
	j := newWithClock(&fakeMover{}, testConfig(), nil, clk)
	defer j.Close()

	j.Stop() // остановка невключённого — не ошибка
	j.Start()
	clk.nextTimer(t)
	j.Stop()
	j.Stop()

	st := j.State()
	if st.Running {
		t.Error("после Stop планировщик должен стоять")
	}
	if !st.NextAt.IsZero() {
		t.Errorf("после Stop время следующего тика должно быть нулевым, получено %v", st.NextAt)
	}
}

// TestNoTickAfterStop — главный инвариант: отложенный таймер, сработавший уже
// после Stop, не должен приводить к движению курсора.
func TestNoTickAfterStop(t *testing.T) {
	clk := newFakeClock()
	m := &fakeMover{}
	j := newWithClock(m, testConfig(), nil, clk)
	defer j.Close()

	j.Start()
	tm := clk.nextTimer(t)
	j.Stop()

	// Таймер «срабатывает» с опозданием — планировщик уже его не слушает.
	tm.ch <- clk.Now()

	// Синхронная команда служит барьером: если бы тик обработался, он успел бы
	// пройти до неё.
	j.Start()
	clk.nextTimer(t)

	if got := m.calls(); len(got) != 0 {
		t.Errorf("после Stop выполнено %d движений, ожидалось 0", len(got))
	}
}

func TestTickJiggles(t *testing.T) {
	clk := newFakeClock()
	m := &fakeMover{}
	cfg := testConfig()
	j := newWithClock(m, cfg, nil, clk)
	defer j.Close()

	j.Start()
	tm := clk.nextTimer(t)
	tm.ch <- clk.Now()
	clk.nextTimer(t) // дождались перепланирования — тик обработан

	calls := m.calls()
	if len(calls) != 1 {
		t.Fatalf("движений: %d, ожидалось 1", len(calls))
	}
	if calls[0] != cfg.Pixels {
		t.Errorf("сдвиг %d px, ожидалось %d", calls[0], cfg.Pixels)
	}
	if st := j.State(); st.Count != 1 {
		t.Errorf("счётчик движений = %d, ожидалось 1", st.Count)
	}
}

func TestSkipWhenUserActive(t *testing.T) {
	clk := newFakeClock()
	m := &fakeMover{}
	m.setIdle(2000, false) // пользователь трогал мышь 2 секунды назад

	cfg := testConfig()
	cfg.SkipWhenUserActive = true
	j := newWithClock(m, cfg, nil, clk)
	defer j.Close()

	j.Start()
	tm := clk.nextTimer(t)
	tm.ch <- clk.Now()
	clk.nextTimer(t)

	if got := m.calls(); len(got) != 0 {
		t.Errorf("при активном пользователе выполнено %d движений, ожидалось 0", len(got))
	}
	st := j.State()
	if st.Skipped != 1 {
		t.Errorf("пропущено тиков: %d, ожидалось 1", st.Skipped)
	}
	if st.Count != 0 {
		t.Errorf("счётчик движений = %d, ожидалось 0", st.Count)
	}
}

// TestOwnInjectIsNotUserActivity — ловушка, ради которой заведён признак ours:
// собственный инжект обновляет счётчик простоя и без этой проверки навсегда
// выглядел бы как работающий пользователь.
func TestOwnInjectIsNotUserActivity(t *testing.T) {
	clk := newFakeClock()
	m := &fakeMover{}
	m.setIdle(10, true) // ввод был только что, но он наш

	cfg := testConfig()
	cfg.SkipWhenUserActive = true
	j := newWithClock(m, cfg, nil, clk)
	defer j.Close()

	j.Start()
	tm := clk.nextTimer(t)
	tm.ch <- clk.Now()
	clk.nextTimer(t)

	if got := m.calls(); len(got) != 1 {
		t.Errorf("движений: %d, ожидалось 1 — свой же инжект не повод пропускать тик", len(got))
	}
}

func TestIdleAboveThresholdJiggles(t *testing.T) {
	clk := newFakeClock()
	m := &fakeMover{}
	m.setIdle(60_000, false) // минута простоя при пороге 30 секунд

	cfg := testConfig()
	cfg.SkipWhenUserActive = true
	j := newWithClock(m, cfg, nil, clk)
	defer j.Close()

	j.Start()
	tm := clk.nextTimer(t)
	tm.ch <- clk.Now()
	clk.nextTimer(t)

	if got := m.calls(); len(got) != 1 {
		t.Errorf("движений: %d, ожидалось 1", len(got))
	}
}

func TestJiggleErrorDoesNotCount(t *testing.T) {
	clk := newFakeClock()
	m := &fakeMover{err: errors.New("SendInput отклонён")}
	j := newWithClock(m, testConfig(), nil, clk)
	defer j.Close()

	j.Start()
	tm := clk.nextTimer(t)
	tm.ch <- clk.Now()
	clk.nextTimer(t)

	if st := j.State(); st.Count != 0 {
		t.Errorf("счётчик движений = %d, ожидалось 0 при ошибке инжекта", st.Count)
	}
}

func TestReconfigureAppliesNewInterval(t *testing.T) {
	clk := newFakeClock()
	j := newWithClock(&fakeMover{}, testConfig(), nil, clk)
	defer j.Close()

	j.Start()
	if d := clk.nextTimer(t).d; d != time.Minute {
		t.Fatalf("исходный интервал %v, ожидалась минута", d)
	}

	cfg := testConfig()
	cfg.Min, cfg.Max = 5*time.Second, 5*time.Second
	j.Reconfigure(cfg)

	if d := clk.nextTimer(t).d; d != 5*time.Second {
		t.Errorf("после Reconfigure интервал %v, ожидалось 5s", d)
	}
	if st := j.State(); !st.Running {
		t.Error("Reconfigure не должен останавливать планировщик")
	}
}

func TestReconfigureWhileStoppedDoesNotStart(t *testing.T) {
	clk := newFakeClock()
	j := newWithClock(&fakeMover{}, testConfig(), nil, clk)
	defer j.Close()

	j.Reconfigure(testConfig())

	if st := j.State(); st.Running {
		t.Error("Reconfigure не должен запускать остановленный планировщик")
	}
	select {
	case <-clk.created:
		t.Error("Reconfigure завёл таймер, хотя планировщик стоит")
	default:
	}
}

func TestNextIntervalWithinBounds(t *testing.T) {
	cfg := Config{Min: 30 * time.Second, Max: 90 * time.Second}

	seen := map[time.Duration]bool{}
	for range 2000 {
		d := nextInterval(cfg)
		if d < cfg.Min || d > cfg.Max {
			t.Fatalf("интервал %v вне границ [%v, %v]", d, cfg.Min, cfg.Max)
		}
		seen[d] = true
	}
	if len(seen) < 100 {
		t.Errorf("интервал почти не меняется: различных значений %d — случайность не работает", len(seen))
	}
}

func TestNextIntervalDegenerateBounds(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want time.Duration
	}{
		{"min == max", Config{Min: time.Minute, Max: time.Minute}, time.Minute},
		{"max меньше min", Config{Min: time.Minute, Max: time.Second}, time.Minute},
		{"нулевой min", Config{Min: 0, Max: 0}, time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextInterval(tt.cfg); got != tt.want {
				t.Errorf("nextInterval = %v, ожидалось %v", got, tt.want)
			}
		})
	}
}

func TestOnStateCalled(t *testing.T) {
	clk := newFakeClock()

	var mu sync.Mutex
	var states []State
	onState := func(s State) {
		mu.Lock()
		defer mu.Unlock()
		states = append(states, s)
	}

	j := newWithClock(&fakeMover{}, testConfig(), onState, clk)
	defer j.Close()

	j.Start()
	clk.nextTimer(t)
	j.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(states) < 3 {
		t.Fatalf("получено %d уведомлений, ожидалось минимум 3 (создание, Start, Stop)", len(states))
	}
	if !states[len(states)-2].Running {
		t.Error("предпоследнее уведомление должно сообщать о работе")
	}
	if states[len(states)-1].Running {
		t.Error("последнее уведомление должно сообщать об остановке")
	}
}

func TestCloseStopsGoroutine(t *testing.T) {
	clk := newFakeClock()
	j := newWithClock(&fakeMover{}, testConfig(), nil, clk)

	j.Start()
	clk.nextTimer(t)
	j.Close()

	if st := j.State(); st.Running {
		t.Error("после Close планировщик должен быть остановлен")
	}
}
