//go:build windows

// Пакет autorun управляет автозапуском приложения вместе с Windows.
//
// Источник истины — сам реестр, а не файл настроек: пользователь может
// отключить автозапуск через «Диспетчер задач → Автозагрузка», и галочка
// в меню трея обязана это увидеть.
package autorun

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`

	// valueName — имя значения в реестре, оно же имя приложения в списке
	// автозагрузки.
	valueName = "Mover"

	// StartupFlag передаётся в команде автозапуска: по нему приложение
	// понимает, что стартовало вместе с системой, и не показывает окно.
	StartupFlag = "-autostart"
)

// IsEnabled сообщает, прописано ли приложение в автозагрузку.
//
// Проверяется только наличие значения: если exe переехал в другой каталог,
// галочка не должна пропадать — путь обновится при следующем включении.
func IsEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("открытие ветки автозагрузки: %w", err)
	}
	defer k.Close()

	if _, _, err := k.GetStringValue(valueName); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("чтение значения %s: %w", valueName, err)
	}
	return true, nil
}

// Enable прописывает приложение в автозагрузку, перезаписывая путь актуальным.
func Enable() error {
	cmd, err := command()
	if err != nil {
		return err
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("открытие ветки автозагрузки на запись: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(valueName, cmd); err != nil {
		return fmt.Errorf("запись автозапуска: %w", err)
	}
	return nil
}

// Disable убирает приложение из автозагрузки.
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("открытие ветки автозагрузки на запись: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("удаление автозапуска: %w", err)
	}
	return nil
}

// Set приводит автозапуск к нужному состоянию.
func Set(on bool) error {
	if on {
		return Enable()
	}
	return Disable()
}

// command собирает строку запуска для реестра.
//
// Путь обязательно в кавычках: без них Windows разберёт «C:\Program Files\…»
// как команду «C:\Program» с аргументами.
func command() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("определение пути к exe: %w", err)
	}
	return fmt.Sprintf("%q %s", filepath.Clean(exe), StartupFlag), nil
}
