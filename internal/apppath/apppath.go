// Пакет apppath выбирает, где приложению держать свои файлы.
//
// По умолчанию всё лежит рядом с exe — так удобнее носить программу с собой.
// Но если exe положили в Program Files или на сетевую шару только для чтения,
// туда писать нельзя, и файл уезжает в профиль пользователя.
package apppath

import (
	"os"
	"path/filepath"
)

// AppDir — имя каталога приложения внутри профиля пользователя.
const AppDir = "mover"

// ExeDir возвращает каталог, в котором лежит исполняемый файл.
//
// Рабочий каталог для этого не годится: при запуске из автозагрузки или по
// клику в трее он совсем другой.
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// Resolve выбирает полный путь к файлу name.
//
// Приоритет у каталога рядом с exe: он берётся, если файл там уже есть или
// каталог доступен на запись. Иначе путь строится от переменной окружения
// envVar (обычно APPDATA или LOCALAPPDATA); нужный подкаталог создаётся.
func Resolve(name, envVar string) string {
	local := filepath.Join(ExeDir(), name)

	if _, err := os.Stat(local); err == nil {
		return local
	}
	if writable(ExeDir()) {
		return local
	}

	base := os.Getenv(envVar)
	if base == "" {
		return local // переменной нет — деваться некуда, пробуем рядом с exe
	}

	dir := filepath.Join(base, AppDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return local
	}
	return filepath.Join(dir, name)
}

// writable проверяет каталог на запись единственным надёжным способом —
// пробует создать в нём временный файл.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".mover-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
