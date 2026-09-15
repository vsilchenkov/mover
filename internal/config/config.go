// Пакет config читает и сохраняет настройки приложения.
//
// Файл лежит рядом с exe, а если туда нельзя писать — в %APPDATA%\mover.
// Отсутствие файла не ошибка: берутся значения по умолчанию, и файл появится
// при первом сохранении из окна настроек.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"mover/internal/apppath"
	"mover/internal/jiggler"
)

// FileName — имя файла настроек.
const FileName = "config.yml"

// Границы значений. Ими же пользуется проверка ввода в окне настроек.
const (
	MinIntervalSec = 5
	MaxIntervalSec = 3600

	MinPixels = 1
	MaxPixels = 50

	MinIdleSec = 5
	MaxIdleSec = 3600
)

// Config — настройки, хранимые в config.yml.
type Config struct {
	IntervalMinSec     int  `yaml:"IntervalMinSec"`
	IntervalMaxSec     int  `yaml:"IntervalMaxSec"`
	MovePixels         int  `yaml:"MovePixels"`
	IdleThresholdSec   int  `yaml:"IdleThresholdSec"`
	SkipWhenUserActive bool `yaml:"SkipWhenUserActive"`
	KeepScreenAwake    bool `yaml:"KeepScreenAwake"`
}

// Flags — параметры командной строки. В файле настроек их нет.
type Flags struct {
	// Debug включает подробный лог.
	Debug bool
	// Autostart выставляется в команде автозагрузки: приложение стартует
	// сразу в трей, без показа окна.
	Autostart bool
}

// Default возвращает настройки по умолчанию.
func Default() Config {
	return Config{
		IntervalMinSec:     30,
		IntervalMaxSec:     90,
		MovePixels:         2,
		IdleThresholdSec:   30,
		SkipWhenUserActive: true,
		KeepScreenAwake:    true,
	}
}

// ParseFlags разбирает командную строку.
func ParseFlags() Flags {
	var f Flags
	flag.BoolVar(&f.Debug, "debug", false, "подробный лог и вывод в stdout")
	flag.BoolVar(&f.Autostart, "autostart", false, "запуск из автозагрузки: сразу в трей, без окна")
	flag.Parse()
	return f
}

// Path возвращает полный путь к файлу настроек.
func Path() string { return apppath.Resolve(FileName, "APPDATA") }

// Load читает настройки. Возвращаемая конфигурация пригодна к работе всегда:
// при отсутствии или порче файла это значения по умолчанию, а ошибка нужна,
// чтобы сообщить о проблеме пользователю.
func Load() (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil // первый запуск — это нормально
		}
		return cfg, fmt.Errorf("чтение %s: %w", Path(), err)
	}

	// Разбор идёт поверх значений по умолчанию, поэтому пропущенные в файле
	// поля не обнуляются.
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("разбор %s: %w", Path(), err)
	}

	cfg.Normalize()
	return cfg, nil
}

// Save записывает настройки на диск.
func Save(cfg Config) error {
	cfg.Normalize()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("сериализация настроек: %w", err)
	}
	if err := os.WriteFile(Path(), data, 0o644); err != nil {
		return fmt.Errorf("запись %s: %w", Path(), err)
	}
	return nil
}

// Normalize приводит значения к допустимым границам.
func (c *Config) Normalize() {
	c.IntervalMinSec = clamp(c.IntervalMinSec, MinIntervalSec, MaxIntervalSec)
	c.IntervalMaxSec = clamp(c.IntervalMaxSec, MinIntervalSec, MaxIntervalSec)
	c.IntervalMaxSec = max(c.IntervalMaxSec, c.IntervalMinSec)
	c.MovePixels = clamp(c.MovePixels, MinPixels, MaxPixels)
	c.IdleThresholdSec = clamp(c.IdleThresholdSec, MinIdleSec, MaxIdleSec)
}

// Jiggler переводит настройки в конфигурацию планировщика.
func (c Config) Jiggler() jiggler.Config {
	return jiggler.Config{
		Min:                time.Duration(c.IntervalMinSec) * time.Second,
		Max:                time.Duration(c.IntervalMaxSec) * time.Second,
		Pixels:             c.MovePixels,
		IdleThreshold:      time.Duration(c.IdleThresholdSec) * time.Second,
		SkipWhenUserActive: c.SkipWhenUserActive,
	}
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}
