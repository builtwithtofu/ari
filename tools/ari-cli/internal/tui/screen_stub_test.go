package tui

import (
	"sync"
	"testing"

	"github.com/gdamore/tcell/v3"
)

type screenCell struct {
	primary   rune
	combining []rune
	style     tcell.Style
}

type testScreen struct {
	mu     sync.RWMutex
	once   sync.Once
	width  int
	height int
	cells  map[[2]int]screenCell
	events chan tcell.Event
}

func newTestScreen(width int, height int) *testScreen {
	return &testScreen{
		width:  width,
		height: height,
		cells:  make(map[[2]int]screenCell),
		events: make(chan tcell.Event, 32),
	}
}

func (s *testScreen) Init() error { return nil }

func (s *testScreen) Fini() {
	s.once.Do(func() {
		close(s.events)
	})
}

func TestTestScreenFiniIsIdempotent(t *testing.T) {
	t.Parallel()

	screen := newTestScreen(2, 2)
	if err := screen.Init(); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	screen.Fini()
	screen.Fini()
}

func (s *testScreen) SetContent(x int, y int, primary rune, combining []rune, style tcell.Style) {
	if x < 0 || y < 0 || x >= s.width || y >= s.height {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cell := screenCell{primary: primary, style: style}
	if len(combining) > 0 {
		cell.combining = append([]rune(nil), combining...)
	}

	s.cells[[2]int{x, y}] = cell
}

func (s *testScreen) Size() (int, int) { return s.width, s.height }

func (s *testScreen) Show() {}

func (s *testScreen) EventQ() <-chan tcell.Event { return s.events }

func (s *testScreen) SetSize(width int, height int) {
	s.width = width
	s.height = height
}

func (s *testScreen) cellAt(x int, y int) screenCell {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.cells[[2]int{x, y}]
}
