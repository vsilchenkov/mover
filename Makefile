# Mover — Makefile.
# Совместим с Windows (GNU Make / mingw32-make).

GO  ?= go
PKG  = ./...
CMD  = ./cmd/mover
APP  = mover

# fyne требует CGO. В системном PATH раньше MSYS2 может стоять
# C:\Program Files\Git\mingw64\bin со старыми DLL (libgmp-10, libwinpthread-1):
# cc1.exe грузит их первыми и падает с 0xC0000139, а cgo сообщает лишь
# «cgo.exe: exit status 2». Подкладываем MSYS2 в начало PATH.
ifeq ($(OS),Windows_NT)
    ifneq ($(wildcard C:/msys64/mingw64/bin/gcc.exe),)
        export PATH := C:\msys64\mingw64\bin;$(PATH)
    endif
endif

.DEFAULT_GOAL := help

.PHONY: help
help: ## Список доступных целей
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ----- проверки --------------------------------------------------------------

.PHONY: test vet lint check

test: ## Юнит-тесты
	$(GO) test -count=1 $(PKG)

vet: ## go vet
	$(GO) vet $(PKG)

lint: ## golangci-lint
	golangci-lint run --timeout=15m

check: vet test ## Полная проверка перед коммитом

# ----- ресурсы ---------------------------------------------------------------

.PHONY: icon

icon: ## Перегенерировать иконки в assets/ (результат коммитится)
	$(GO) run ./cmd/genicon -out assets

# ----- сборка ----------------------------------------------------------------

.PHONY: build build-win run clean

build: ## Отладочная сборка: с консолью, без ресурса версии
	$(GO) build -o $(APP)-debug.exe $(CMD)

build-win: test ## Релизная сборка mover.exe: иконка, версия, манифест, без консоли
	-rm -f cmd/mover/resource.syso
	cd cmd/mover && goversioninfo -64 versioninfo.json
	$(GO) build -ldflags "-H=windowsgui -s -w" -o $(APP).exe $(CMD)
	-rm -f cmd/mover/resource.syso

run: ## Запуск из исходников с консолью и логом в stdout
	$(GO) run $(CMD) -debug

clean: ## Удалить артефакты сборки и тестов
	$(GO) clean -testcache
	-rm -f $(APP).exe $(APP)-debug.exe smoke.exe cmd/mover/resource.syso
	-rm -f coverage.out coverage.html
