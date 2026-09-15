//go:build windows

package ui

import (
	"syscall"
	"unsafe"
)

// Релизная сборка идёт с -H=windowsgui: у процесса нет консоли, и вывод
// в stderr уходит в никуда. Единственный способ сообщить о фатальной ошибке
// до появления окна — системное окно сообщения.
const (
	mbOK          = 0x00000000
	mbIconError   = 0x00000010
	mbSystemModal = 0x00001000
)

var procMessageBoxW = user32.NewProc("MessageBoxW")

// Fatal показывает окно с сообщением об ошибке.
//
// Пригодно и до инициализации fyne: обращается напрямую к user32.
func Fatal(title, text string) {
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	body, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}

	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(body)),
		uintptr(unsafe.Pointer(t)),
		mbOK|mbIconError|mbSystemModal,
	)
}
