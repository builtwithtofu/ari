package tui

import (
	"testing"

	"github.com/gdamore/tcell/v3"
)

func TestDefaultThemeHasAllRoles(t *testing.T) {
	t.Parallel()

	fgAccent := DefaultTheme.Accent.GetForeground()
	if fgAccent == tcell.ColorDefault {
		t.Fatal("DefaultTheme.Accent foreground = default, want themed color")
	}

	bgStatus := DefaultTheme.StatusBg.GetBackground()
	if bgStatus == tcell.ColorDefault {
		t.Fatal("DefaultTheme.StatusBg background = default, want themed color")
	}

	fgError := DefaultTheme.Error.GetForeground()
	if fgError == tcell.ColorDefault {
		t.Fatal("DefaultTheme.Error foreground = default, want themed color")
	}
}

func TestBuiltinThemesIncludesKnownPalettes(t *testing.T) {
	t.Parallel()

	builtins := BuiltinThemes()
	if _, ok := builtins["catppuccin-mocha"]; !ok {
		t.Fatal("catppuccin-mocha theme missing")
	}

	if _, ok := builtins["nord"]; !ok {
		t.Fatal("nord theme missing")
	}

	if _, ok := builtins["dracula"]; !ok {
		t.Fatal("dracula theme missing")
	}
}

func TestWidgetCanUseThemeRoleStyles(t *testing.T) {
	t.Parallel()

	screen := newSimulationScreen(t, 2, 1)
	widget := &themeRoleWidget{}
	widget.Paint(screen, Rect{X: 0, Y: 0, Width: 2, Height: 1}, &DefaultTheme)

	fgCell := screen.cellAt(0, 0).style.GetForeground()
	fgAccent := DefaultTheme.Accent.GetForeground()
	if fgCell != fgAccent {
		t.Fatalf("widget style foreground = %v, want %v", fgCell, fgAccent)
	}

	if got := simulationRune(t, screen, 0, 0); got != 'W' {
		t.Fatalf("widget rune = %q, want %q", string(got), "W")
	}
}

type themeRoleWidget struct{}

func (w *themeRoleWidget) Paint(screen Screen, rect Rect, theme *Theme) {
	if screen == nil || theme == nil {
		return
	}

	screen.SetContent(rect.X, rect.Y, 'W', nil, theme.Accent)
}
