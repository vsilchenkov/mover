// Пакет logging настраивает slog для приложения.
//
// В штатной работе журнал молчит: пишутся только предупреждения и ошибки,
// а сам файл создаётся лишь тогда, когда действительно есть что записать, —
// в каталоге с программой не появляется ничего лишнего.
//
// Совсем отказаться от журнала нельзя: релизная сборка идёт с -H=windowsgui,
// у неё нет ни stdout, ни stderr, и без файла любая ошибка запуска осталась бы
// незамеченной. Подробный вывод включается флагом -debug.
package logging

import (
	"io"
	"log/slog"
	"os"
	"sync"

	"mover/internal/apppath"
)

// maxLogSize — порог ротации. Приложение живёт сутками, и без ограничения
// журнал рос бы бесконечно.
const maxLogSize = 1 << 20 // 1 МБ

// logName — имя файла журнала рядом с exe либо в %LOCALAPPDATA%\mover.
const logName = "app.log"

// Config — параметры инициализации логгера.
type Config struct {
	// Debug понижает порог до уровня Debug и дублирует вывод в stdout.
	// Без него в файл попадают только предупреждения и ошибки.
	Debug bool
}

// path — куда пишется журнал; нужен для сообщений об ошибках.
var path string

// Init настраивает логгер по умолчанию.
func Init(c Config) {
	path = apppath.Resolve(logName, "LOCALAPPDATA")

	var output io.Writer = &lazyFile{path: path}
	level := slog.LevelWarn

	if c.Debug {
		level = slog.LevelDebug
		output = io.MultiWriter(output, os.Stdout)
	}

	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.String(a.Key, a.Value.Time().Format("2006-01-02 15:04:05"))
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}

// Path возвращает путь к файлу журнала.
func Path() string { return path }

// Err оборачивает ошибку в атрибут с единым именем во всём приложении.
func Err(err error) slog.Attr { return slog.Any("error", err) }

// lazyFile открывает файл только при первой записи.
//
// Пока приложению нечего сказать, файла не существует вовсе; ротация тоже
// откладывается до этого момента.
type lazyFile struct {
	path string

	mu     sync.Mutex
	file   *os.File
	failed bool
}

func (w *lazyFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if w.failed {
			// Открыть не удалось — молча не пишем. Ронять приложение
			// из-за недоступного журнала было бы явным перебором.
			return len(p), nil
		}

		rotate(w.path)

		f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			w.failed = true
			return len(p), nil
		}
		w.file = f
	}

	return w.file.Write(p)
}

// rotate переносит разросшийся журнал в <имя>.1, оставляя одно поколение истории.
func rotate(p string) {
	info, err := os.Stat(p)
	if err != nil || info.Size() < maxLogSize {
		return
	}
	old := p + ".1"
	_ = os.Remove(old)    // Windows не даст переименовать поверх существующего
	_ = os.Rename(p, old) // не получилось — не беда, журнал просто продолжит расти
}
