package tui

import "github.com/gdamore/tcell/v3"

// Theme maps semantic UI roles to concrete styles.
//
// Widgets consume these roles instead of hard-coding colors so the same widget
// paint logic can be reused across multiple palettes.
type Theme struct {
	Accent        tcell.Style
	Muted         tcell.Style
	StatusBg      tcell.Style
	StatusFg      tcell.Style
	OverlayBg     tcell.Style
	OverlayBorder tcell.Style
	Error         tcell.Style
}

var DefaultTheme = CatppuccinMocha()

func BuiltinThemes() map[string]Theme {
	return map[string]Theme{
		"catppuccin-mocha": CatppuccinMocha(),
		"nord":             Nord(),
		"dracula":          Dracula(),
		"gruvbox-dark":     GruvboxDark(),
	}
}

func CatppuccinMocha() Theme {
	base := tcell.GetColor("#1e1e2e")
	mantle := tcell.GetColor("#181825")
	lavender := tcell.GetColor("#b4befe")
	subtext := tcell.GetColor("#a6adc8")
	overlay0 := tcell.GetColor("#6c7086")
	peach := tcell.GetColor("#fab387")

	return Theme{
		Accent:        tcell.StyleDefault.Foreground(lavender).Bold(true),
		Muted:         tcell.StyleDefault.Foreground(subtext),
		StatusBg:      tcell.StyleDefault.Background(mantle),
		StatusFg:      tcell.StyleDefault.Foreground(lavender).Background(mantle),
		OverlayBg:     tcell.StyleDefault.Background(base),
		OverlayBorder: tcell.StyleDefault.Foreground(overlay0).Background(base),
		Error:         tcell.StyleDefault.Foreground(peach).Background(base).Bold(true),
	}
}

func Nord() Theme {
	base := tcell.GetColor("#2e3440")
	muted := tcell.GetColor("#4c566a")
	accent := tcell.GetColor("#81a1c1")
	errorColor := tcell.GetColor("#bf616a")

	return Theme{
		Accent:        tcell.StyleDefault.Foreground(accent).Bold(true),
		Muted:         tcell.StyleDefault.Foreground(muted),
		StatusBg:      tcell.StyleDefault.Background(base),
		StatusFg:      tcell.StyleDefault.Foreground(accent).Background(base),
		OverlayBg:     tcell.StyleDefault.Background(base),
		OverlayBorder: tcell.StyleDefault.Foreground(muted).Background(base),
		Error:         tcell.StyleDefault.Foreground(errorColor).Background(base).Bold(true),
	}
}

func Dracula() Theme {
	base := tcell.GetColor("#282a36")
	muted := tcell.GetColor("#6272a4")
	accent := tcell.GetColor("#bd93f9")
	errorColor := tcell.GetColor("#ff5555")

	return Theme{
		Accent:        tcell.StyleDefault.Foreground(accent).Bold(true),
		Muted:         tcell.StyleDefault.Foreground(muted),
		StatusBg:      tcell.StyleDefault.Background(base),
		StatusFg:      tcell.StyleDefault.Foreground(accent).Background(base),
		OverlayBg:     tcell.StyleDefault.Background(base),
		OverlayBorder: tcell.StyleDefault.Foreground(muted).Background(base),
		Error:         tcell.StyleDefault.Foreground(errorColor).Background(base).Bold(true),
	}
}

func GruvboxDark() Theme {
	base := tcell.GetColor("#282828")
	muted := tcell.GetColor("#928374")
	accent := tcell.GetColor("#83a598")
	errorColor := tcell.GetColor("#fb4934")

	return Theme{
		Accent:        tcell.StyleDefault.Foreground(accent).Bold(true),
		Muted:         tcell.StyleDefault.Foreground(muted),
		StatusBg:      tcell.StyleDefault.Background(base),
		StatusFg:      tcell.StyleDefault.Foreground(accent).Background(base),
		OverlayBg:     tcell.StyleDefault.Background(base),
		OverlayBorder: tcell.StyleDefault.Foreground(muted).Background(base),
		Error:         tcell.StyleDefault.Foreground(errorColor).Background(base).Bold(true),
	}
}
