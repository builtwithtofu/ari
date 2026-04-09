package tui

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v3"
)

// Screen is the minimal rendering contract used by this package.
//
// It stays intentionally narrow so the TUI core can be extracted without
// coupling external callers to the full tcell Screen interface.
type Screen interface {
	SetContent(x int, y int, primary rune, combining []rune, style tcell.Style)
	Size() (width int, height int)
	Show()
	EventQ() <-chan tcell.Event
}

// AdaptTCellScreen wraps a tcell screen with the package Screen contract.
func AdaptTCellScreen(screen tcell.Screen) Screen {
	if screen == nil {
		return nil
	}

	return &tcellScreenAdapter{screen: screen}
}

type tcellScreenAdapter struct {
	screen tcell.Screen
}

func (a *tcellScreenAdapter) SetContent(x int, y int, primary rune, combining []rune, style tcell.Style) {
	a.screen.SetContent(x, y, primary, combining, style)
}

func (a *tcellScreenAdapter) Size() (int, int) {
	return a.screen.Size()
}

func (a *tcellScreenAdapter) Show() {
	a.screen.Show()
}

func (a *tcellScreenAdapter) EventQ() <-chan tcell.Event {
	return a.screen.EventQ()
}

// Rect describes a rectangular draw region in screen coordinates.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Widget paints into the provided region using semantic theme styles.
type Widget interface {
	Paint(screen Screen, rect Rect, theme *Theme)
}

// VTSource is the rendering dependency needed by Loop.
type VTSource interface {
	CopyTo(screen Screen, x int, y int, width int, height int) error
	Resize(cols int, rows int) error
	SetOverlayActive(active bool)
	Dirty() bool
	MarkClean()
}

// Loop coordinates VT-copy, widget paint, and screen flushes.
//
// The loop tracks an explicit dirty flag and only flushes when content changed.
// This keeps no-op frames effectively free while still allowing immediate input
// handling through the screen event channel.
type Loop struct {
	mu        sync.Mutex
	screen    Screen
	vt        VTSource
	theme     *Theme
	statusBar Widget
	overlay   bool
	dirty     bool
	showFn    func()
}

func NewLoop(screen Screen, vt VTSource, theme *Theme, statusBar Widget) (*Loop, error) {
	if screen == nil {
		return nil, fmt.Errorf("screen is required")
	}

	if vt == nil {
		return nil, fmt.Errorf("vt source is required")
	}

	if theme == nil {
		return nil, fmt.Errorf("theme is required")
	}

	loop := &Loop{
		screen:    screen,
		vt:        vt,
		theme:     theme,
		statusBar: statusBar,
		dirty:     true,
	}
	loop.showFn = screen.Show

	return loop, nil
}

func (l *Loop) EventQ() <-chan tcell.Event {
	if l == nil || l.screen == nil {
		return nil
	}

	return l.screen.EventQ()
}

func (l *Loop) MarkDirty() {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.dirty = true
}

func (l *Loop) SetOverlayActive(active bool) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.overlay == active {
		return
	}

	l.overlay = active
	l.vt.SetOverlayActive(active)
	l.dirty = true
}

func (l *Loop) Resize(cols int, rows int) error {
	if l == nil {
		return fmt.Errorf("loop is required")
	}

	if cols <= 0 {
		return fmt.Errorf("cols must be greater than zero")
	}

	statusBarHeight := l.statusBarHeight()
	minimumRows := statusBarHeight + 1
	if rows < minimumRows {
		return fmt.Errorf("rows must be at least %d", minimumRows)
	}

	if err := l.vt.Resize(cols, rows-statusBarHeight); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.dirty = true
	return nil
}

// Frame draws one frame when the screen is dirty.
//
// It returns true when a Show() flush happens.
func (l *Loop) Frame() (bool, error) {
	if l == nil {
		return false, fmt.Errorf("loop is required")
	}

	if l.screen == nil {
		return false, fmt.Errorf("screen is required")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	shouldRender := l.dirty || l.vt.Dirty()
	if !shouldRender {
		return false, nil
	}

	width, height := l.screen.Size()
	if width <= 0 {
		return false, fmt.Errorf("screen width must be greater than zero")
	}

	if height <= 0 {
		return false, fmt.Errorf("screen height must be greater than zero")
	}

	statusBarHeight := l.statusBarHeight()
	viewportHeight := height - statusBarHeight
	if viewportHeight < 0 {
		viewportHeight = 0
	}

	if !l.overlay {
		if err := l.vt.CopyTo(l.screen, 0, 0, width, viewportHeight); err != nil {
			return false, err
		}
	}

	if l.statusBar != nil && statusBarHeight > 0 {
		l.statusBar.Paint(l.screen, Rect{X: 0, Y: height - 1, Width: width, Height: 1}, l.theme)
	}

	l.showFn()
	l.vt.MarkClean()
	l.dirty = false

	return true, nil
}

func (l *Loop) statusBarHeight() int {
	if l.statusBar == nil {
		return 0
	}

	return 1
}
