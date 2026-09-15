package config

import (
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	before := cfg
	cfg.Normalize()

	if cfg != before {
		t.Errorf("значения по умолчанию не проходят собственную проверку: было %+v, стало %+v", before, cfg)
	}
}

func TestNormalizeClamps(t *testing.T) {
	tests := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "слишком маленький интервал поднимается до минимума",
			in:   Config{IntervalMinSec: 1, IntervalMaxSec: 2, MovePixels: 2, IdleThresholdSec: 30},
			want: Config{IntervalMinSec: MinIntervalSec, IntervalMaxSec: MinIntervalSec, MovePixels: 2, IdleThresholdSec: 30},
		},
		{
			name: "слишком большой интервал опускается до максимума",
			in:   Config{IntervalMinSec: 99999, IntervalMaxSec: 99999, MovePixels: 2, IdleThresholdSec: 30},
			want: Config{IntervalMinSec: MaxIntervalSec, IntervalMaxSec: MaxIntervalSec, MovePixels: 2, IdleThresholdSec: 30},
		},
		{
			name: "верхняя граница не может быть ниже нижней",
			in:   Config{IntervalMinSec: 90, IntervalMaxSec: 30, MovePixels: 2, IdleThresholdSec: 30},
			want: Config{IntervalMinSec: 90, IntervalMaxSec: 90, MovePixels: 2, IdleThresholdSec: 30},
		},
		{
			name: "нулевой сдвиг превращается в минимальный",
			in:   Config{IntervalMinSec: 30, IntervalMaxSec: 90, MovePixels: 0, IdleThresholdSec: 30},
			want: Config{IntervalMinSec: 30, IntervalMaxSec: 90, MovePixels: MinPixels, IdleThresholdSec: 30},
		},
		{
			name: "чрезмерный сдвиг обрезается",
			in:   Config{IntervalMinSec: 30, IntervalMaxSec: 90, MovePixels: 1000, IdleThresholdSec: 30},
			want: Config{IntervalMinSec: 30, IntervalMaxSec: 90, MovePixels: MaxPixels, IdleThresholdSec: 30},
		},
		{
			name: "порог простоя приводится к границам",
			in:   Config{IntervalMinSec: 30, IntervalMaxSec: 90, MovePixels: 2, IdleThresholdSec: 0},
			want: Config{IntervalMinSec: 30, IntervalMaxSec: 90, MovePixels: 2, IdleThresholdSec: MinIdleSec},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			got.Normalize()
			if got != tt.want {
				t.Errorf("Normalize дал %+v, ожидалось %+v", got, tt.want)
			}
		})
	}
}

func TestJigglerConversion(t *testing.T) {
	cfg := Config{
		IntervalMinSec:     30,
		IntervalMaxSec:     90,
		MovePixels:         3,
		IdleThresholdSec:   45,
		SkipWhenUserActive: true,
	}

	jc := cfg.Jiggler()

	if jc.Min != 30*time.Second {
		t.Errorf("Min = %v, ожидалось 30s", jc.Min)
	}
	if jc.Max != 90*time.Second {
		t.Errorf("Max = %v, ожидалось 90s", jc.Max)
	}
	if jc.IdleThreshold != 45*time.Second {
		t.Errorf("IdleThreshold = %v, ожидалось 45s", jc.IdleThreshold)
	}
	if jc.Pixels != 3 {
		t.Errorf("Pixels = %d, ожидалось 3", jc.Pixels)
	}
	if !jc.SkipWhenUserActive {
		t.Error("SkipWhenUserActive потерялся при конвертации")
	}
}
