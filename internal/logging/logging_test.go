package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLazyFileNotCreatedWithoutWrites проверяет, что файл не появляется,
// пока писать нечего: в каталоге с программой не должно заводиться пустых
// журналов.
func TestLazyFileNotCreatedWithoutWrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.log")
	w := &lazyFile{path: p}

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("файл существует до первой записи: %v", err)
	}

	// Обращение к Write — единственный повод создать файл.
	if _, err := w.Write([]byte("важное\n")); err != nil {
		t.Fatalf("запись: %v", err)
	}
	t.Cleanup(func() {
		if w.file != nil {
			w.file.Close()
		}
	})

	if _, err := os.Stat(p); err != nil {
		t.Errorf("после записи файла нет: %v", err)
	}
}

// TestLazyFileSurvivesUnwritablePath — недоступный каталог не должен ронять
// приложение: журнал просто не ведётся.
func TestLazyFileSurvivesUnwritablePath(t *testing.T) {
	// Путь заведомо некорректен: внутри существующего файла каталога нет.
	file := filepath.Join(t.TempDir(), "занято")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	w := &lazyFile{path: filepath.Join(file, "app.log")}

	n, err := w.Write([]byte("сообщение\n"))
	if err != nil {
		t.Errorf("недоступный журнал вернул ошибку: %v", err)
	}
	if n == 0 {
		t.Error("Write должен отчитаться о принятых байтах, даже если писать некуда")
	}
}

// TestQuietByDefault — главное свойство: в обычном режиме в журнал попадают
// только предупреждения и ошибки, а INFO и DEBUG отбрасываются.
func TestQuietByDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.log")
	w := &lazyFile{path: p}
	t.Cleanup(func() {
		if w.file != nil {
			w.file.Close()
		}
	})

	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelWarn}))

	logger.Debug("подробность")
	logger.Info("обычное событие")

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("файл создан из-за сообщений уровня Info — журнал должен молчать")
	}

	logger.Warn("вот это уже важно")

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("после предупреждения файла нет: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "вот это уже важно") {
		t.Error("предупреждение не записано")
	}
	if strings.Contains(text, "обычное событие") || strings.Contains(text, "подробность") {
		t.Error("в журнал просочились сообщения ниже уровня предупреждения")
	}
}

// TestRotate проверяет, что разросшийся журнал уезжает в .1 и не растёт вечно.
func TestRotate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")

	big := make([]byte, maxLogSize+1)
	if err := os.WriteFile(p, big, 0o644); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	rotate(p)

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("исходный файл должен был уехать в .1")
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Errorf("поколения .1 нет: %v", err)
	}
}

// TestRotateKeepsSmallLog — небольшой журнал трогать не за чем.
func TestRotateKeepsSmallLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(p, []byte("немного\n"), 0o644); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	rotate(p)

	if _, err := os.Stat(p); err != nil {
		t.Errorf("маленький журнал не должен был исчезнуть: %v", err)
	}
}
