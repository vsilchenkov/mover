//go:build windows

package autorun

import (
	"strings"
	"testing"
)

// TestCommandQuoting стережёт формат строки автозапуска. Без кавычек Windows
// разберёт путь «C:\Program Files\...» как команду «C:\Program» с аргументами,
// и приложение молча не будет стартовать вместе с системой.
func TestCommandQuoting(t *testing.T) {
	cmd, err := command()
	if err != nil {
		t.Fatalf("command: %v", err)
	}

	if !strings.HasPrefix(cmd, `"`) {
		t.Errorf("путь не в кавычках: %s", cmd)
	}
	if !strings.HasSuffix(cmd, " "+StartupFlag) {
		t.Errorf("нет флага %s в конце команды: %s", StartupFlag, cmd)
	}

	path := strings.TrimSuffix(cmd, " "+StartupFlag)
	if !strings.HasPrefix(path, `"`) || !strings.HasSuffix(path, `"`) {
		t.Errorf("путь закавычен не полностью: %s", path)
	}
	if strings.Count(path, `"`) != 2 {
		t.Errorf("лишние кавычки в пути: %s", path)
	}
	if !strings.HasSuffix(strings.ToLower(strings.Trim(path, `"`)), ".exe") {
		t.Errorf("команда не указывает на exe: %s", path)
	}
}

// TestIsEnabledReadable проверяет, что состояние автозапуска читается без
// ошибки: ветка HKCU\...\Run существует у любого пользователя.
func TestIsEnabledReadable(t *testing.T) {
	if _, err := IsEnabled(); err != nil {
		t.Errorf("не удалось прочитать состояние автозапуска: %v", err)
	}
}
