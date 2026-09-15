// Команда genicon рисует иконки приложения на чистом Go и раскладывает их в assets/:
//
//	icon.ico         — многоразмерная иконка для exe (DIB 16..64 + PNG 128/256);
//	icon-active.png  — иконка трея во включённом состоянии;
//	icon-idle.png    — иконка трея в остановленном состоянии.
//
// Рисование идёт с четырёхкратным суперсэмплингом: каждый размер рендерится
// в size*ss пикселей и усредняется блоками ss×ss. Композитинг и усреднение
// выполняются в premultiplied-alpha, иначе по краям скруглений появляется кайма.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

// ss — кратность суперсэмплинга.
const ss = 4

// Размеры записей в .ico. До dibMaxSize включительно пишем DIB, крупнее — PNG.
var icoSizes = []int{16, 24, 32, 48, 64, 128, 256}

const dibMaxSize = 64

// trayIconSize — fyne масштабирует иконку трея к 64 пикселям (systrayIconSize).
const trayIconSize = 64

// palette описывает цвета одного варианта иконки.
type palette struct {
	top    color.NRGBA // верх градиента подложки
	bottom color.NRGBA // низ градиента подложки
	arrow  color.NRGBA // курсор
	dash   color.NRGBA // штрихи движения
}

var (
	activePalette = palette{
		top:    color.NRGBA{0x4F, 0x46, 0xE5, 0xFF},
		bottom: color.NRGBA{0x7C, 0x7A, 0xFF, 0xFF},
		arrow:  color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF},
		dash:   color.NRGBA{0xFF, 0xFF, 0xFF, 0xD9},
	}
	idlePalette = palette{
		top:    color.NRGBA{0x5A, 0x61, 0x6B, 0xFF},
		bottom: color.NRGBA{0x86, 0x8E, 0x99, 0xFF},
		arrow:  color.NRGBA{0xFF, 0xFF, 0xFF, 0xE6},
		dash:   color.NRGBA{0xFF, 0xFF, 0xFF, 0xA6},
	}
)

func main() {
	out := flag.String("out", "assets", "каталог, куда положить иконки")
	flag.Parse()

	if err := run(*out); err != nil {
		log.Fatalf("genicon: %v", err)
	}
}

func run(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("создание каталога %s: %w", dir, err)
	}

	ico, err := buildICO(icoSizes, activePalette)
	if err != nil {
		return fmt.Errorf("сборка ico: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.ico"), ico, 0o644); err != nil {
		return err
	}
	fmt.Printf("icon.ico        %d размеров, %d байт\n", len(icoSizes), len(ico))

	for _, v := range []struct {
		name string
		pal  palette
	}{
		{"icon-active.png", activePalette},
		{"icon-idle.png", idlePalette},
	} {
		data, err := encodePNG(render(trayIconSize, v.pal))
		if err != nil {
			return fmt.Errorf("кодирование %s: %w", v.name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, v.name), data, 0o644); err != nil {
			return err
		}
		fmt.Printf("%-15s %dx%d, %d байт\n", v.name, trayIconSize, trayIconSize, len(data))
	}
	return nil
}

// ----- рисование -------------------------------------------------------------

// px — пиксель в premultiplied-alpha, компоненты в диапазоне 0..1.
type px struct{ r, g, b, a float64 }

// over накладывает src поверх p по правилу source-over.
func (p px) over(src px) px {
	inv := 1 - src.a
	return px{
		r: src.r + p.r*inv,
		g: src.g + p.g*inv,
		b: src.b + p.b*inv,
		a: src.a + p.a*inv,
	}
}

// premul переводит цвет со straight-alpha в premultiplied.
func premul(c color.NRGBA) px {
	a := float64(c.A) / 255
	return px{
		r: float64(c.R) / 255 * a,
		g: float64(c.G) / 255 * a,
		b: float64(c.B) / 255 * a,
		a: a,
	}
}

// point — точка в нормированных координатах 0..1, ось Y направлена вниз.
type point struct{ x, y float64 }

// arrowShape — классический курсор-стрелка в собственных координатах 0..1.
// Остриё в (0,0), дальше левый край, вырез и «хвост».
var arrowShape = []point{
	{0.00, 0.00}, {0.00, 0.72}, {0.19, 0.56}, {0.30, 0.83},
	{0.44, 0.77}, {0.33, 0.50}, {0.55, 0.50},
}

const (
	arrowX     = 0.17 // сдвиг стрелки по X
	arrowY     = 0.15 // сдвиг стрелки по Y
	arrowScale = 0.50 // масштаб стрелки внутри подложки
	cornerR    = 0.22 // радиус скругления подложки
)

// dashes — штрихи движения справа от курсора: x0, y0, x1, y1 (нормированные).
var dashes = [][4]float64{
	{0.66, 0.33, 0.87, 0.40},
	{0.66, 0.53, 0.79, 0.60},
}

// render рисует иконку заданного размера с суперсэмплингом.
func render(size int, pal palette) *image.NRGBA {
	n := size * ss
	buf := make([]px, n*n)

	top, bottom := premul(pal.top), premul(pal.bottom)
	arrow, dash := premul(pal.arrow), premul(pal.dash)

	poly := make([]point, len(arrowShape))
	for i, p := range arrowShape {
		poly[i] = point{arrowX + p.x*arrowScale, arrowY + p.y*arrowScale}
	}

	for y := range n {
		v := (float64(y) + 0.5) / float64(n)
		// Вертикальный градиент подложки.
		bg := px{
			r: top.r + (bottom.r-top.r)*v,
			g: top.g + (bottom.g-top.g)*v,
			b: top.b + (bottom.b-top.b)*v,
			a: top.a + (bottom.a-top.a)*v,
		}
		for x := range n {
			u := (float64(x) + 0.5) / float64(n)
			if !insideRoundRect(u, v, cornerR) {
				continue // за пределами подложки — прозрачно
			}
			c := bg
			switch {
			case insidePolygon(point{u, v}, poly):
				c = c.over(arrow)
			case insideDash(u, v):
				c = c.over(dash)
			}
			buf[y*n+x] = c
		}
	}

	return downsample(buf, n, size)
}

// insideRoundRect проверяет попадание точки в скруглённый квадрат 0..1.
func insideRoundRect(x, y, r float64) bool {
	// Ближайший центр скругления по каждой оси.
	cx, cy := x, y
	switch {
	case x < r:
		cx = r
	case x > 1-r:
		cx = 1 - r
	}
	switch {
	case y < r:
		cy = r
	case y > 1-r:
		cy = 1 - r
	}
	if cx == x || cy == y {
		return x >= 0 && x <= 1 && y >= 0 && y <= 1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// insidePolygon — тест «точка в многоугольнике» лучом по правилу чётности.
func insidePolygon(p point, poly []point) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		pi, pj := poly[i], poly[j]
		if (pi.y > p.y) != (pj.y > p.y) &&
			p.x < (pj.x-pi.x)*(p.y-pi.y)/(pj.y-pi.y)+pi.x {
			in = !in
		}
	}
	return in
}

// insideDash проверяет попадание в один из штрихов движения (капсула).
func insideDash(x, y float64) bool {
	for _, d := range dashes {
		x0, y0, x1, y1 := d[0], d[1], d[2], d[3]
		r := (y1 - y0) / 2
		cy := (y0 + y1) / 2
		if distToSegment(x, y, x0+r, cy, x1-r, cy) <= r {
			return true
		}
	}
	return false
}

// distToSegment — расстояние от точки до отрезка.
func distToSegment(px0, py0, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = ((px0-ax)*dx + (py0-ay)*dy) / l2
		t = math.Max(0, math.Min(1, t))
	}
	qx, qy := ax+t*dx, ay+t*dy
	return math.Hypot(px0-qx, py0-qy)
}

// downsample усредняет блоки ss×ss в premultiplied-alpha и переводит
// результат в straight-alpha NRGBA.
func downsample(buf []px, n, size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	inv := 1.0 / float64(ss*ss)
	for y := range size {
		for x := range size {
			var acc px
			for dy := range ss {
				row := (y*ss + dy) * n
				for dx := range ss {
					s := buf[row+x*ss+dx]
					acc.r += s.r
					acc.g += s.g
					acc.b += s.b
					acc.a += s.a
				}
			}
			acc.r *= inv
			acc.g *= inv
			acc.b *= inv
			acc.a *= inv

			out := color.NRGBA{}
			if acc.a > 0 {
				out.R = clamp8(acc.r / acc.a)
				out.G = clamp8(acc.g / acc.a)
				out.B = clamp8(acc.b / acc.a)
				out.A = clamp8(acc.a)
			}
			img.SetNRGBA(x, y, out)
		}
	}
	return img
}

func clamp8(v float64) uint8 {
	n := int(math.Round(v * 255))
	switch {
	case n < 0:
		return 0
	case n > 255:
		return 255
	}
	return uint8(n)
}

// ----- контейнер .ico --------------------------------------------------------

// icoDirEntrySize — размер одной записи ICONDIRENTRY.
const icoDirEntrySize = 16

// buildICO собирает многоразмерный .ico. Размеры до dibMaxSize пишутся как DIB,
// крупнее — как PNG: так поступают штатные инструменты Windows, и такой файл
// безопасно читают не только проводник, но и сторонние извлекатели иконок.
func buildICO(sizes []int, pal palette) ([]byte, error) {
	type entry struct {
		size int
		data []byte
	}
	entries := make([]entry, 0, len(sizes))
	for _, s := range sizes {
		img := render(s, pal)
		var data []byte
		var err error
		if s <= dibMaxSize {
			data = encodeDIB(img)
		} else {
			data, err = encodePNG(img)
		}
		if err != nil {
			return nil, fmt.Errorf("размер %d: %w", s, err)
		}
		entries = append(entries, entry{size: s, data: data})
	}

	var buf bytes.Buffer
	// ICONDIR: reserved, type=1 (иконка), count.
	writeLE(&buf, uint16(0), uint16(1), uint16(len(entries)))

	offset := uint32(6 + icoDirEntrySize*len(entries))
	for _, e := range entries {
		// Размер 256 записывается как 0 — в байт он не помещается.
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0
		}
		buf.WriteByte(dim)        // bWidth
		buf.WriteByte(dim)        // bHeight
		buf.WriteByte(0)          // bColorCount: 0 для true color
		buf.WriteByte(0)          // bReserved
		writeLE(&buf, uint16(1))  // wPlanes
		writeLE(&buf, uint16(32)) // wBitCount
		writeLE(&buf, uint32(len(e.data)), offset)
		offset += uint32(len(e.data))
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return buf.Bytes(), nil
}

// encodeDIB кодирует изображение как BITMAPINFOHEADER + XOR-битмап + AND-маска.
// Высота в заголовке удваивается, строки идут снизу вверх, AND-маска обязательна
// даже для 32bpp и выравнивается по 4 байта на строку.
func encodeDIB(img *image.NRGBA) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	var buf bytes.Buffer
	writeLE(&buf,
		uint32(40),    // biSize
		int32(w),      // biWidth
		int32(2*h),    // biHeight: XOR + AND
		uint16(1),     // biPlanes
		uint16(32),    // biBitCount
		uint32(0),     // biCompression = BI_RGB
		uint32(w*h*4), // biSizeImage
		int32(0),      // biXPelsPerMeter
		int32(0),      // biYPelsPerMeter
		uint32(0),     // biClrUsed
		uint32(0),     // biClrImportant
	)

	// XOR: BGRA со straight-alpha, строки снизу вверх.
	for y := h - 1; y >= 0; y-- {
		for x := range w {
			c := img.NRGBAAt(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}

	// AND-маска: 1 бит на пиксель, строка выравнивается по 4 байта.
	// Нули означают «пиксель показывать» — прозрачностью управляет альфа XOR.
	stride := ((w + 31) / 32) * 4
	mask := make([]byte, stride*h)
	buf.Write(mask)

	return buf.Bytes()
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeLE пишет значения в little-endian; ошибок у bytes.Buffer не бывает.
func writeLE(buf *bytes.Buffer, vals ...any) {
	for _, v := range vals {
		if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
			panic(err) // недостижимо: bytes.Buffer не возвращает ошибок
		}
	}
}
