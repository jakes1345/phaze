package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Phaze7Theme is the shared design system across the desktop + Android Fyne
// builds. The palette and spacing scale mirror the web client tokens so the
// product feels like one app on every surface:
//
//	Web (CSS)                        Fyne (Go)
//	--brand              #863bff     PhazeBrand
//	--text               #1a1a1a     PhazeText (light)
//	--panel              #ffffff     PhazePanel (light)
//	--panel-edge         #e5e5ea     PhazeSeparator
//	--shell              #f5f5f7     PhazeShell
//	(dark: shell #000000, panel #111111)
//
// Brand color names PhazeBlue/PhazeLightBlue/etc. are kept as aliases for
// existing call sites elsewhere in the package, but every alias now points
// at the modernized palette so the look upgrades uniformly without grep-ing
// the rest of the codebase.
type Phaze7Theme struct{}

// --- Light palette ----------------------------------------------------------
var (
	PhazeBrand      = color.NRGBA{R: 0x86, G: 0x3B, B: 0xFF, A: 0xFF}
	PhazeBrandHover = color.NRGBA{R: 0x6F, G: 0x1E, B: 0xE0, A: 0xFF}
	PhazeBrandSoft  = color.NRGBA{R: 0x86, G: 0x3B, B: 0xFF, A: 0x1A}

	PhazeShell     = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF7, A: 0xFF}
	PhazePanel     = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	PhazeText      = color.NRGBA{R: 0x1A, G: 0x1A, B: 0x1A, A: 0xFF}
	PhazeMuted     = color.NRGBA{R: 0x8E, G: 0x8E, B: 0x93, A: 0xFF}
	PhazeSeparator = color.NRGBA{R: 0xE5, G: 0xE5, B: 0xEA, A: 0xFF}
	PhazeHover     = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x0A}
	PhazeBubbleIn  = color.NRGBA{R: 0xE9, G: 0xE9, B: 0xEB, A: 0xFF}

	// Legacy aliases mapped to the modern palette so existing code keeps working.
	PhazeBlue      = PhazeBrand
	PhazeLightBlue = PhazeBrandSoft
	PhazeLightGray = PhazeHover
	PhazeDarkText  = PhazeText
)

// --- Dark palette (true-black shell for OLED) -------------------------------
var (
	PhazeBrandDark      = color.NRGBA{R: 0xA6, G: 0x77, B: 0xFF, A: 0xFF}
	PhazeBrandHoverDark = color.NRGBA{R: 0xB8, G: 0x8E, B: 0xFF, A: 0xFF}
	PhazeBrandSoftDark  = color.NRGBA{R: 0xA6, G: 0x77, B: 0xFF, A: 0x1E}

	PhazeShellDark     = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF}
	PhazePanelDark     = color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xFF}
	PhazeTextDark      = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF7, A: 0xFF}
	PhazeMutedDark     = color.NRGBA{R: 0x63, G: 0x63, B: 0x66, A: 0xFF}
	PhazeSeparatorDark = color.NRGBA{R: 0x1C, G: 0x1C, B: 0x1E, A: 0xFF}
	PhazeHoverDark     = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x0D}
	PhazeBubbleInDark  = color.NRGBA{R: 0x1C, G: 0x1C, B: 0x1E, A: 0xFF}
)

func (m Phaze7Theme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	dark := variant == theme.VariantDark
	switch name {
	case theme.ColorNameBackground:
		if dark {
			return PhazeShellDark
		}
		return PhazeShell
	case theme.ColorNameOverlayBackground:
		if dark {
			return PhazePanelDark
		}
		return PhazePanel
	case theme.ColorNameMenuBackground:
		if dark {
			return PhazePanelDark
		}
		return PhazePanel
	case theme.ColorNameInputBackground:
		if dark {
			return PhazePanelDark
		}
		return PhazePanel
	case theme.ColorNameInputBorder:
		if dark {
			return PhazeSeparatorDark
		}
		return PhazeSeparator
	case theme.ColorNamePrimary:
		if dark {
			return PhazeBrandDark
		}
		return PhazeBrand
	case theme.ColorNameButton:
		if dark {
			return PhazeHoverDark
		}
		return PhazeHover
	case theme.ColorNameForeground:
		if dark {
			return PhazeTextDark
		}
		return PhazeText
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	case theme.ColorNameDisabled:
		if dark {
			return color.NRGBA{R: 0x52, G: 0x52, B: 0x58, A: 0xFF}
		}
		return color.NRGBA{R: 0xB5, G: 0xB5, B: 0xBC, A: 0xFF}
	case theme.ColorNameDisabledButton:
		if dark {
			return color.NRGBA{R: 0x18, G: 0x18, B: 0x1C, A: 0xFF}
		}
		return color.NRGBA{R: 0xF0, G: 0xF0, B: 0xF3, A: 0xFF}
	case theme.ColorNamePlaceHolder:
		if dark {
			return PhazeMutedDark
		}
		return PhazeMuted
	case theme.ColorNameHover:
		if dark {
			return PhazeHoverDark
		}
		return PhazeHover
	case theme.ColorNameSelection:
		if dark {
			return PhazeBrandSoftDark
		}
		return PhazeBrandSoft
	case theme.ColorNameFocus:
		if dark {
			return PhazeBrandDark
		}
		return PhazeBrand
	case theme.ColorNamePressed:
		if dark {
			return PhazeBrandHoverDark
		}
		return PhazeBrandHover
	case theme.ColorNameScrollBar:
		if dark {
			return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x33}
		}
		return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x33}
	case theme.ColorNameSeparator:
		if dark {
			return PhazeSeparatorDark
		}
		return PhazeSeparator
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x0F, G: 0x12, B: 0x18, A: 0x14}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 0x10, G: 0xB9, B: 0x81, A: 0xFF}
	case theme.ColorNameWarning:
		return color.NRGBA{R: 0xF5, G: 0x9E, B: 0x0B, A: 0xFF}
	case theme.ColorNameError:
		return color.NRGBA{R: 0xDC, G: 0x26, B: 0x26, A: 0xFF}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (m Phaze7Theme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

// Font preserves the bundled Tahoma asset path so existing builds still load
// it from the vault. If Inter.ttf ever ships in the vault, swap the path
// here. We keep one font for all styles — Fyne renders weight via its own
// system fallback.
func (m Phaze7Theme) Font(style fyne.TextStyle) fyne.Resource {
	// Try Inter first (if a future build bundles it), then Tahoma, then default.
	for _, path := range []string{"fonts/Inter.ttf", "fonts/Tahoma.ttf"} {
		r := GetAssetResource(path)
		if len(r.Content()) > 1024 {
			return r
		}
	}
	return theme.DefaultTheme().Font(style)
}

func (m Phaze7Theme) Size(name fyne.ThemeSizeName) float32 {
	mobile := IsMobile()
	switch name {
	case theme.SizeNamePadding:
		if mobile {
			return 12
		}
		return 10
	case theme.SizeNameInnerPadding:
		if mobile {
			return 10
		}
		return 8
	case theme.SizeNameText:
		if mobile {
			return 16
		}
		return 14
	case theme.SizeNameHeadingText:
		if mobile {
			return 22
		}
		return 20
	case theme.SizeNameSubHeadingText:
		if mobile {
			return 18
		}
		return 16
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameCaptionText:
		if mobile {
			return 13
		}
		return 11
	case theme.SizeNameInlineIcon:
		if mobile {
			return 22
		}
		return 18
	case theme.SizeNameScrollBar:
		if mobile {
			return 6
		}
		return 10
	case theme.SizeNameSeparatorThickness:
		return 1
	}
	return theme.DefaultTheme().Size(name)
}
