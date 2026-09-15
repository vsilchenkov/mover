// Пакет assets содержит встроенные в бинарник ресурсы приложения.
//
// Иконки генерируются командой cmd/genicon (цель `make icon`) и коммитятся
// в репозиторий: сборка их не перегенерирует.
package assets

import _ "embed"

// IconActive — иконка трея, когда таймер работает.
//
//go:embed icon-active.png
var IconActive []byte

// IconIdle — иконка трея, когда таймер остановлен.
//
//go:embed icon-idle.png
var IconIdle []byte
