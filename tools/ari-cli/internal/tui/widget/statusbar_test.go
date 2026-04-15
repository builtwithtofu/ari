package widget

import (
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/tui"
	"github.com/gdamore/tcell/v3"
)

func TestStatusBarPaintsBottomRowWithThemeStyles(t *testing.T) {
	screen := &statusBarTestScreen{cells: map[[2]int]statusBarCell{}}

	bar := NewStatusBar("agent-1 attached")
	theme := tui.DefaultTheme
	bar.Paint(screen, tui.Rect{X: 0, Y: 1, Width: 12, Height: 1}, &theme)

	r, style := screen.contentAt(0, 1)
	if r != 'a' {
		t.Fatalf("first rune = %q, want %q", string(r), "a")
	}
	if style != theme.StatusFg {
		t.Fatalf("style = %#v, want %#v", style, theme.StatusFg)
	}

	topRune, _ := screen.contentAt(0, 0)
	if topRune != 0 {
		t.Fatalf("top row rune = %q, want empty", string(topRune))
	}
}

type statusBarCell struct {
	rune  rune
	style tcell.Style
}

type statusBarTestScreen struct {
	cells map[[2]int]statusBarCell
}

func (s *statusBarTestScreen) SetContent(x int, y int, primary rune, _ []rune, style tcell.Style) {
	key := [2]int{x, y}
	s.cells[key] = statusBarCell{rune: primary, style: style}
}

func (s *statusBarTestScreen) Size() (width int, height int) {
	return 12, 2
}

func (s *statusBarTestScreen) Show() {}

func (s *statusBarTestScreen) EventQ() <-chan tcell.Event {
	return nil
}

func (s *statusBarTestScreen) contentAt(x int, y int) (rune, tcell.Style) {
	cell, ok := s.cells[[2]int{x, y}]
	if !ok {
		return 0, tcell.StyleDefault
	}
	return cell.rune, cell.style
}
