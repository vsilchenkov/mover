package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

// TestBuildICOStructure разбирает сгенерированный .ico обратно и проверяет
// поля, на которых Windows молча ломается: Planes, BitCount и согласованность
// смещений с размерами данных.
func TestBuildICOStructure(t *testing.T) {
	data, err := buildICO(icoSizes, activePalette)
	if err != nil {
		t.Fatalf("buildICO: %v", err)
	}

	r := bytes.NewReader(data)
	var reserved, typ, count uint16
	mustRead(t, r, &reserved, &typ, &count)

	if reserved != 0 {
		t.Errorf("ICONDIR.reserved = %d, ожидалось 0", reserved)
	}
	if typ != 1 {
		t.Errorf("ICONDIR.type = %d, ожидалось 1 (иконка)", typ)
	}
	if int(count) != len(icoSizes) {
		t.Fatalf("записей в директории %d, ожидалось %d", count, len(icoSizes))
	}

	type dirEntry struct {
		width, height      byte
		colorCount, unused byte
		planes, bitCount   uint16
		bytesInRes, offset uint32
	}

	entries := make([]dirEntry, count)
	for i := range entries {
		e := &entries[i]
		mustRead(t, r, &e.width, &e.height, &e.colorCount, &e.unused,
			&e.planes, &e.bitCount, &e.bytesInRes, &e.offset)
	}

	wantOffset := uint32(6 + icoDirEntrySize*len(icoSizes))
	for i, e := range entries {
		size := icoSizes[i]

		wantDim := byte(size)
		if size >= 256 {
			wantDim = 0 // 256 записывается нулём
		}
		if e.width != wantDim || e.height != wantDim {
			t.Errorf("размер %d: width/height = %d/%d, ожидалось %d", size, e.width, e.height, wantDim)
		}
		if e.colorCount != 0 {
			t.Errorf("размер %d: colorCount = %d, ожидалось 0", size, e.colorCount)
		}
		if e.unused != 0 {
			t.Errorf("размер %d: reserved = %d, ожидалось 0", size, e.unused)
		}
		if e.planes != 1 {
			t.Errorf("размер %d: planes = %d, ожидалось 1", size, e.planes)
		}
		if e.bitCount != 32 {
			t.Errorf("размер %d: bitCount = %d, ожидалось 32", size, e.bitCount)
		}
		if e.offset != wantOffset {
			t.Errorf("размер %d: offset = %d, ожидалось %d", size, e.offset, wantOffset)
		}
		if e.offset+e.bytesInRes > uint32(len(data)) {
			t.Fatalf("размер %d: данные выходят за пределы файла", size)
		}
		wantOffset += e.bytesInRes
	}

	if int(wantOffset) != len(data) {
		t.Errorf("суммарная длина %d, а файл занимает %d байт", wantOffset, len(data))
	}
}

// TestICOEntryPayloads проверяет содержимое записей: мелкие размеры — DIB
// с удвоенной высотой и AND-маской, крупные — валидный PNG.
func TestICOEntryPayloads(t *testing.T) {
	data, err := buildICO(icoSizes, activePalette)
	if err != nil {
		t.Fatalf("buildICO: %v", err)
	}

	offset := 6 + icoDirEntrySize*len(icoSizes)
	for _, size := range icoSizes {
		var bytesInRes uint32
		entryAt := 6 + icoDirEntrySize*indexOf(icoSizes, size)
		bytesInRes = binary.LittleEndian.Uint32(data[entryAt+8:])
		payload := data[offset : offset+int(bytesInRes)]
		offset += int(bytesInRes)

		if size > dibMaxSize {
			cfg, err := png.DecodeConfig(bytes.NewReader(payload))
			if err != nil {
				t.Errorf("размер %d: PNG не декодируется: %v", size, err)
				continue
			}
			if cfg.Width != size || cfg.Height != size {
				t.Errorf("размер %d: PNG %dx%d", size, cfg.Width, cfg.Height)
			}
			continue
		}

		if len(payload) < 40 {
			t.Errorf("размер %d: DIB короче заголовка", size)
			continue
		}
		biSize := binary.LittleEndian.Uint32(payload[0:])
		biWidth := int32(binary.LittleEndian.Uint32(payload[4:]))
		biHeight := int32(binary.LittleEndian.Uint32(payload[8:]))
		biPlanes := binary.LittleEndian.Uint16(payload[12:])
		biBitCount := binary.LittleEndian.Uint16(payload[14:])
		biCompression := binary.LittleEndian.Uint32(payload[16:])

		if biSize != 40 {
			t.Errorf("размер %d: biSize = %d, ожидалось 40", size, biSize)
		}
		if int(biWidth) != size {
			t.Errorf("размер %d: biWidth = %d", size, biWidth)
		}
		if int(biHeight) != 2*size {
			t.Errorf("размер %d: biHeight = %d, ожидалось %d (XOR+AND)", size, biHeight, 2*size)
		}
		if biPlanes != 1 || biBitCount != 32 {
			t.Errorf("размер %d: planes/bitCount = %d/%d", size, biPlanes, biBitCount)
		}
		if biCompression != 0 {
			t.Errorf("размер %d: biCompression = %d, ожидалось 0 (BI_RGB)", size, biCompression)
		}

		maskStride := ((size + 31) / 32) * 4
		want := 40 + size*size*4 + maskStride*size
		if len(payload) != want {
			t.Errorf("размер %d: DIB занимает %d байт, ожидалось %d (заголовок+XOR+AND)",
				size, len(payload), want)
		}
	}
}

// TestRenderAlpha проверяет, что углы иконки прозрачны (скругление работает),
// а центр — нет.
func TestRenderAlpha(t *testing.T) {
	img := render(64, activePalette)

	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("угол непрозрачен: alpha = %d, ожидалось 0", a)
	}
	if a := img.NRGBAAt(32, 32).A; a != 255 {
		t.Errorf("центр прозрачен: alpha = %d, ожидалось 255", a)
	}
}

func indexOf(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func mustRead(t *testing.T, r *bytes.Reader, vals ...any) {
	t.Helper()
	for _, v := range vals {
		if err := binary.Read(r, binary.LittleEndian, v); err != nil {
			t.Fatalf("чтение: %v", err)
		}
	}
}
