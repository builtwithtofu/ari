package widget

import (
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/tui"
)

// StatusBar renders a single-line status string at the bottom row.
type StatusBar struct {
	text string
}

// NewStatusBar creates a status bar with initial text.
func NewStatusBar(text string) *StatusBar {
	return &StatusBar{text: strings.TrimSpace(text)}
}

// SetText updates the status text.
func (s *StatusBar) SetText(text string) {
	if s == nil {
		return
	}

	s.text = strings.TrimSpace(text)
}

// Paint draws one styled row and overlays status text from the left.
func (s *StatusBar) Paint(screen tui.Screen, rect tui.Rect, theme *tui.Theme) {
	if screen == nil || theme == nil {
		return
	}
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}

	for x := rect.X; x < rect.X+rect.Width; x++ {
		screen.SetContent(x, rect.Y, ' ', nil, theme.StatusBg)
	}

	text := s.statusText()
	for i, r := range text {
		if i >= rect.Width {
			break
		}
		screen.SetContent(rect.X+i, rect.Y, r, nil, theme.StatusFg)
	}
}

func (s *StatusBar) statusText() string {
	if s == nil {
		return ""
	}
	return s.text
}
