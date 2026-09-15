//go:build windows

// Команда mover не даёт Windows заблокировать экран: через случайные
// промежутки времени она чуть двигает курсор, сбрасывая системный таймер
// простоя. Управление — окно с кнопками и иконка в системном трее.
package main

import (
	"fmt"
	"log/slog"
	"runtime/debug"

	"mover/internal/config"
	"mover/internal/instance"
	"mover/internal/logging"
	"mover/internal/ui"
)

func main() {
	flags := config.ParseFlags()

	// Логгер поднимаем первым: со сборкой -H=windowsgui у приложения нет
	// ни stdout, ни stderr, и без файла журнала любая ошибка старта будет немой.
	// В обычном режиме журнал молчит — пишутся только предупреждения и ошибки.
	logging.Init(logging.Config{Debug: flags.Debug})

	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			slog.Error("паника", slog.Any("panic", r), slog.String("stack", string(stack)))
			ui.Fatal("Mover", fmt.Sprintf("Аварийное завершение: %v\n\nПодробности в %s", r, logging.Path()))
		}
	}()

	slog.Info("запуск",
		slog.Bool("автозагрузка", flags.Autostart),
		slog.String("конфиг", config.Path()),
		slog.String("лог", logging.Path()))

	cfg, err := config.Load()
	if err != nil {
		// Настройки испорчены или недоступны — работаем на значениях
		// по умолчанию, но след в логе оставляем.
		slog.Warn("настройки не прочитаны, взяты значения по умолчанию", logging.Err(err))
	}

	inst, first, err := instance.Acquire()
	if err != nil {
		slog.Error("не удалось проверить единственность экземпляра", logging.Err(err))
		ui.Fatal("Mover", "Не удалось запуститься: "+err.Error())
		return
	}

	if !first {
		// Второй экземпляр не спорит, а просто просит первый показать окно:
		// пользователь кликнул по ярлыку именно за этим.
		slog.Info("приложение уже запущено — показываем окно первого экземпляра")
		if err := inst.NotifyExisting(); err != nil {
			slog.Error("не удалось разбудить первый экземпляр", logging.Err(err))
		}
		inst.Release()
		return
	}
	defer inst.Release()

	ui.Run(cfg, flags, inst)
	slog.Info("работа завершена")
}
