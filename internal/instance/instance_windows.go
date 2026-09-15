//go:build windows

// Пакет instance следит, чтобы приложение работало в одном экземпляре.
//
// Просто сказать «уже запущено» и выйти — плохой сценарий: пользователь кликает
// по ярлыку именно потому, что хочет увидеть окно. Поэтому кроме мьютекса
// заведён именованный event: второй экземпляр подаёт сигнал и завершается,
// а первый по этому сигналу показывает своё окно.
package instance

import (
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

const (
	// Префикс Local делает объекты видимыми в пределах сеанса пользователя:
	// в другом сеансе RDP приложение должно запускаться самостоятельно.
	mutexName = `Local\mover.single`
	eventName = `Local\mover.show`
)

// Instance удерживает объекты синхронизации до конца работы процесса.
type Instance struct {
	mutex windows.Handle

	// show — именованное событие, общее для всех экземпляров приложения.
	show windows.Handle
	// quit — безымянное событие, видимое только этому процессу. Оно снимает
	// наблюдателя с ожидания при завершении. Отдельное событие принципиально:
	// сигнал в общее разбудил бы чужой экземпляр, и тот решил бы, что его
	// просят показать окно.
	quit windows.Handle

	closed atomic.Bool
}

// Acquire пытается занять роль единственного экземпляра.
// Второй возвращаемый параметр равен false, если приложение уже запущено.
func Acquire() (*Instance, bool, error) {
	mName, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false, fmt.Errorf("имя мьютекса: %w", err)
	}

	// CreateMutex отдаёт годный дескриптор и ERROR_ALREADY_EXISTS одновременно:
	// ошибка здесь означает не сбой, а факт наличия первого экземпляра.
	mutex, err := windows.CreateMutex(nil, false, mName)
	alreadyRunning := errors.Is(err, windows.ERROR_ALREADY_EXISTS)
	if mutex == 0 {
		return nil, false, fmt.Errorf("создание мьютекса: %w", err)
	}

	eName, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		windows.CloseHandle(mutex)
		return nil, false, fmt.Errorf("имя события: %w", err)
	}

	// Событие со сбросом вручную не нужно: автосброс сам снимает сигнал,
	// как только ожидающая сторона его получила.
	show, err := windows.CreateEvent(nil, 0, 0, eName)
	if show == 0 {
		windows.CloseHandle(mutex)
		return nil, false, fmt.Errorf("создание события: %w", err)
	}

	// Событие остановки — безымянное и со сбросом вручную: его достаточно
	// взвести один раз, и наблюдатель гарантированно его увидит.
	quit, err := windows.CreateEvent(nil, 1, 0, nil)
	if quit == 0 {
		windows.CloseHandle(show)
		windows.CloseHandle(mutex)
		return nil, false, fmt.Errorf("создание события остановки: %w", err)
	}

	return &Instance{mutex: mutex, show: show, quit: quit}, !alreadyRunning, nil
}

// NotifyExisting просит уже работающий экземпляр показать своё окно.
func (i *Instance) NotifyExisting() error {
	if err := windows.SetEvent(i.show); err != nil {
		return fmt.Errorf("сигнал первому экземпляру: %w", err)
	}
	return nil
}

// WatchShowRequests запускает наблюдателя за сигналами от других экземпляров.
// Колбэк вызывается из отдельной горутины — обновление интерфейса в нём нужно
// оборачивать в fyne.Do.
func (i *Instance) WatchShowRequests(show func()) {
	go func() {
		handles := []windows.Handle{i.show, i.quit}

		for {
			ev, err := windows.WaitForMultipleObjects(handles, false, windows.INFINITE)
			if i.closed.Load() {
				return
			}
			if err != nil {
				slog.Warn("ожидание сигнала прервано", slog.Any("error", err))
				return
			}

			switch ev {
			case windows.WAIT_OBJECT_0:
				slog.Info("запрос на показ окна от второго экземпляра")
				show()
			case windows.WAIT_OBJECT_0 + 1:
				return // штатная остановка
			default:
				slog.Warn("неожиданный код ожидания", slog.Uint64("code", uint64(ev)))
				return
			}
		}
	}()
}

// Release освобождает объекты синхронизации. Вызывать при завершении работы.
func (i *Instance) Release() {
	i.closed.Store(true)
	// Снимаем наблюдателя с ожидания через собственное событие: общее трогать
	// нельзя, иначе разбудим чужой экземпляр.
	windows.SetEvent(i.quit)
	windows.CloseHandle(i.quit)
	windows.CloseHandle(i.show)
	windows.CloseHandle(i.mutex)
}
