// Пакет jiggler — планировщик micro-движений курсора.
//
// Состоянием владеет одна горутина, все внешние вызовы приходят к ней командами
// по каналу. Это избавляет от россыпи мьютексов и, главное, гарантирует, что
// после возврата из Stop отложенный тик уже не сработает.
package jiggler

import (
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// Mover — то, что умеет двигать курсор и оценивать простой системы.
// Интерфейс позволяет тестировать планировщик без обращения к Windows API.
type Mover interface {
	// Jiggle сдвигает курсор на px пикселей и возвращает обратно.
	Jiggle(px int) error
	// IdleMillis возвращает время простоя системы и признак того, что
	// последний ввод — собственный инжект, а не действие пользователя.
	IdleMillis() (idle uint32, ours bool)
}

// Config — параметры работы планировщика.
type Config struct {
	Min                time.Duration // нижняя граница случайного интервала
	Max                time.Duration // верхняя граница случайного интервала
	Pixels             int           // величина сдвига курсора
	IdleThreshold      time.Duration // сколько система должна простаивать
	SkipWhenUserActive bool          // пропускать тик, если пользователь работает
}

// State — снимок состояния для интерфейса.
type State struct {
	Running   bool      // работает ли планировщик
	NextAt    time.Time // время следующего тика; нулевое, если остановлен
	Interval  time.Duration
	Count     int       // сколько движений сделано с момента запуска
	StartedAt time.Time // когда запущен; нулевое, если остановлен
	Skipped   int       // сколько тиков пропущено из-за активности пользователя
}

// Jiggler планирует движения курсора со случайным интервалом.
type Jiggler struct {
	mover   Mover
	clk     clock
	onState func(State)

	cmds chan command
	snap atomic.Pointer[State]
}

type cmdKind int

const (
	cmdStart cmdKind = iota
	cmdStop
	cmdReconfig
	cmdClose
)

type command struct {
	kind cmdKind
	cfg  Config
	done chan struct{}
}

// New создаёт планировщик и запускает его горутину. Планировщик создаётся
// остановленным; onState вызывается из его горутины при каждом изменении
// состояния и может быть nil.
func New(m Mover, cfg Config, onState func(State)) *Jiggler {
	return newWithClock(m, cfg, onState, realClock{})
}

func newWithClock(m Mover, cfg Config, onState func(State), clk clock) *Jiggler {
	j := &Jiggler{
		mover:   m,
		clk:     clk,
		onState: onState,
		cmds:    make(chan command),
	}
	j.snap.Store(&State{})
	go j.loop(cfg)
	return j
}

// Start запускает планировщик. Повторный вызов ничего не меняет.
// Возврат означает, что команда уже обработана горутиной.
func (j *Jiggler) Start() { j.send(command{kind: cmdStart}) }

// Stop останавливает планировщик. Повторный вызов ничего не меняет.
// После возврата отложенный тик гарантированно не сработает.
func (j *Jiggler) Stop() { j.send(command{kind: cmdStop}) }

// Reconfigure меняет параметры на ходу, не прерывая работу. Новый интервал
// применяется к следующему тику.
func (j *Jiggler) Reconfigure(cfg Config) { j.send(command{kind: cmdReconfig, cfg: cfg}) }

// Close останавливает планировщик и завершает его горутину.
func (j *Jiggler) Close() { j.send(command{kind: cmdClose}) }

// State возвращает последний снимок состояния. Безопасен из любой горутины.
func (j *Jiggler) State() State { return *j.snap.Load() }

func (j *Jiggler) send(c command) {
	c.done = make(chan struct{})
	j.cmds <- c
	<-c.done
}

func (j *Jiggler) loop(cfg Config) {
	var (
		st   State
		tmr  timer
		fire <-chan time.Time
	)

	stopTimer := func() {
		if tmr != nil {
			tmr.Stop()
			tmr = nil
		}
		fire = nil
	}

	schedule := func() {
		stopTimer()
		d := nextInterval(cfg)
		st.Interval = d
		st.NextAt = j.clk.Now().Add(d)
		tmr = j.clk.NewTimer(d)
		fire = tmr.C()
	}

	publish := func() {
		snapshot := st
		j.snap.Store(&snapshot)
		if j.onState != nil {
			j.onState(snapshot)
		}
	}

	publish()

	for {
		select {
		case c := <-j.cmds:
			switch c.kind {
			case cmdStart:
				if !st.Running {
					st.Running = true
					st.StartedAt = j.clk.Now()
					st.Count = 0
					st.Skipped = 0
					schedule()
					slog.Info("планировщик запущен",
						slog.Duration("min", cfg.Min), slog.Duration("max", cfg.Max))
				}

			case cmdStop:
				if st.Running {
					st.Running = false
					st.NextAt = time.Time{}
					st.StartedAt = time.Time{}
					stopTimer()
					slog.Info("планировщик остановлен", slog.Int("движений", st.Count))
				}

			case cmdReconfig:
				cfg = c.cfg
				if st.Running {
					schedule() // новый интервал вступает в силу сразу
				}

			case cmdClose:
				stopTimer()
				st.Running = false
				publish()
				close(c.done)
				return
			}

			// Публикуем до разблокировки вызывающего. Иначе Start() и Stop()
			// возвращают управление раньше, чем состояние ушло в интерфейс,
			// и кнопки успевают остаться в прежнем виде: «СТОП» так и стоит
			// недоступной, и остановить запущенный таймер уже нечем.
			//
			// Взаимной блокировки тут нет: onState обязан только поставить
			// работу в очередь (в интерфейсе это fyne.Do), но не ждать её
			// выполнения — иначе он будет ждать поток, который сам ждёт нас.
			publish()
			close(c.done)

		case <-fire:
			fire = nil
			j.tick(cfg, &st)
			schedule()
			publish()
		}
	}
}

// tick выполняет одно движение либо пропускает его, если за компьютером работают.
func (j *Jiggler) tick(cfg Config, st *State) {
	if cfg.SkipWhenUserActive && j.userActive(cfg) {
		st.Skipped++
		slog.Debug("тик пропущен: пользователь активен")
		return
	}

	if err := j.mover.Jiggle(cfg.Pixels); err != nil {
		// Инжект отклоняется, когда в фокусе окно с более высоким уровнем
		// целостности или показан secure desktop UAC. Это штатная ситуация:
		// пропускаем тик и ждём следующего.
		slog.Warn("движение курсора не выполнено", slog.Any("error", err))
		return
	}
	st.Count++
}

// userActive отвечает, работает ли за компьютером живой пользователь.
//
// Собственные инжекты обновляют счётчик простоя наравне с настоящим вводом,
// поэтому одного времени простоя недостаточно — нужен признак «ввод наш».
func (j *Jiggler) userActive(cfg Config) bool {
	idle, ours := j.mover.IdleMillis()
	if ours {
		return false
	}
	return time.Duration(idle)*time.Millisecond < cfg.IdleThreshold
}

// nextInterval выбирает случайную задержку в пределах [Min, Max].
func nextInterval(cfg Config) time.Duration {
	min, max := cfg.Min, cfg.Max
	if min <= 0 {
		min = time.Second
	}
	if max <= min {
		return min
	}
	return min + rand.N(max-min+1)
}

// ----- часы ------------------------------------------------------------------

// timer — минимум от *time.Timer, нужный планировщику.
type timer interface {
	C() <-chan time.Time
	Stop()
}

// clock абстрагирует время, чтобы тесты не ждали реальных секунд.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) timer
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) timer { return &realTimer{t: time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time { return r.t.C }

func (r *realTimer) Stop() { r.t.Stop() }
