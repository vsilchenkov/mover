package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// palette — набор цветов одного оформления.
type palette struct {
	background color.NRGBA // фон окна
	surface    color.NRGBA // карточки и поля ввода
	foreground color.NRGBA // основной текст
	muted      color.NRGBA // подписи и второстепенный текст
	primary    color.NRGBA // акцент
	success    color.NRGBA // «работает»
	danger     color.NRGBA // «остановить»
	separator  color.NRGBA
	hover      color.NRGBA
}

var (
	lightPalette = palette{
		background: color.NRGBA{0xF6, 0xF7, 0xF9, 0xFF},
		surface:    color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF},
		foreground: color.NRGBA{0x1A, 0x1D, 0x21, 0xFF},
		muted:      color.NRGBA{0x6B, 0x72, 0x80, 0xFF},
		primary:    color.NRGBA{0x4F, 0x46, 0xE5, 0xFF},
		success:    color.NRGBA{0x16, 0xA3, 0x4A, 0xFF},
		danger:     color.NRGBA{0xDC, 0x26, 0x26, 0xFF},
		separator:  color.NRGBA{0xE5, 0xE7, 0xEB, 0xFF},
		hover:      color.NRGBA{0x1A, 0x1D, 0x21, 0x14},
	}
	darkPalette = palette{
		background: color.NRGBA{0x15, 0x18, 0x1D, 0xFF},
		surface:    color.NRGBA{0x1E, 0x22, 0x2A, 0xFF},
		foreground: color.NRGBA{0xE8, 0xEA, 0xED, 0xFF},
		muted:      color.NRGBA{0x9A, 0xA1, 0xAC, 0xFF},
		primary:    color.NRGBA{0x7C, 0x7A, 0xFF, 0xFF},
		success:    color.NRGBA{0x22, 0xC5, 0x5E, 0xFF},
		danger:     color.NRGBA{0xEF, 0x44, 0x44, 0xFF},
		separator:  color.NRGBA{0x2A, 0x2F, 0x38, 0xFF},
		hover:      color.NRGBA{0xFF, 0xFF, 0xFF, 0x14},
	}
)

// moverTheme — оформление приложения. Вариант (светлый или тёмный) fyne
// передаёт в Color сам, отслеживая настройку Windows, поэтому отдельного
// переключателя не нужно.
type moverTheme struct{}

var _ fyne.Theme = moverTheme{}

// paletteFor возвращает палитру для текущего варианта оформления.
func paletteFor(v fyne.ThemeVariant) palette {
	if v == theme.VariantLight {
		return lightPalette
	}
	return darkPalette
}

// currentPalette — палитра, действующая прямо сейчас; нужна виджетам,
// которые рисуют себя сами (индикатор состояния).
func currentPalette() palette {
	return paletteFor(fyne.CurrentApp().Settings().ThemeVariant())
}

func (moverTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	p := paletteFor(v)

	switch name {
	case theme.ColorNameBackground:
		return p.background
	case theme.ColorNameForeground:
		return p.foreground
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return p.muted
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return p.primary
	case theme.ColorNameSuccess:
		return p.success
	case theme.ColorNameError:
		return p.danger
	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		return p.separator
	case theme.ColorNameInputBackground, theme.ColorNameHeaderBackground:
		return p.surface
	case theme.ColorNameButton, theme.ColorNameDisabledButton:
		return p.surface
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return p.surface
	case theme.ColorNameHover:
		return p.hover
	case theme.ColorNameSelection:
		return withAlpha(p.primary, 0x33)
	}
	return theme.DefaultTheme().Color(name, v)
}

func (moverTheme) Font(s fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(s) }

func (moverTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (moverTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameCaptionText:
		return 12
	// Скруглённые углы — главный признак современного интерфейса
	// на фоне стандартной темы fyne.
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameButtonRadius:
		return 10
	case theme.SizeNameCardRadius:
		return 12
	case theme.SizeNameSeparatorThickness:
		return 1
	}
	return theme.DefaultTheme().Size(name)
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}
