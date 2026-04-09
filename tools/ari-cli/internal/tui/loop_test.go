package tui

import (
	"sync"
	"testing"
)

func TestLoopFrameSkipsShowWhenClean(t *testing.T) {
	t.Parallel()

	screen := newSimulationScreen(t, 10, 4)
	vt := &fakeVTSource{}
	loop, err := NewLoop(screen, vt, &DefaultTheme, nil)
	if err != nil {
		t.Fatalf("NewLoop returned error: %v", err)
	}

	// First frame consumes startup dirty state.
	if _, err := loop.Frame(); err != nil {
		t.Fatalf("initial Frame returned error: %v", err)
	}

	showCalls := 0
	loop.showFn = func() { showCalls++ }

	rendered, err := loop.Frame()
	if err != nil {
		t.Fatalf("Frame returned error: %v", err)
	}
	if rendered {
		t.Fatal("rendered = true, want false for clean frame")
	}

	if showCalls != 0 {
		t.Fatalf("show call count = %d, want 0", showCalls)
	}
}

func TestLoopFrameShowsWhenDirty(t *testing.T) {
	t.Parallel()

	screen := newSimulationScreen(t, 10, 4)
	vt := &fakeVTSource{dirty: true}
	widget := &markerWidget{r: 'S'}
	loop, err := NewLoop(screen, vt, &DefaultTheme, widget)
	if err != nil {
		t.Fatalf("NewLoop returned error: %v", err)
	}

	showCalls := 0
	loop.showFn = func() { showCalls++ }

	rendered, err := loop.Frame()
	if err != nil {
		t.Fatalf("Frame returned error: %v", err)
	}
	if !rendered {
		t.Fatal("rendered = false, want true for dirty frame")
	}

	if showCalls != 1 {
		t.Fatalf("show call count = %d, want 1", showCalls)
	}

	if got := simulationRune(t, screen, 0, 3); got != 'S' {
		t.Fatalf("status bar rune = %q, want %q", string(got), "S")
	}
}

func TestLoopResizeMarksDirtyAndResizesVT(t *testing.T) {
	t.Parallel()

	screen := newSimulationScreen(t, 20, 8)
	vt := &fakeVTSource{}
	loop, err := NewLoop(screen, vt, &DefaultTheme, &markerWidget{r: 'S'})
	if err != nil {
		t.Fatalf("NewLoop returned error: %v", err)
	}

	if err := loop.Resize(20, 8); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	if vt.resizeCols != 20 {
		t.Fatalf("resize cols = %d, want 20", vt.resizeCols)
	}

	if vt.resizeRows != 7 {
		t.Fatalf("resize rows = %d, want 7", vt.resizeRows)
	}

	if !loop.dirty {
		t.Fatal("loop dirty = false, want true after Resize")
	}
}

func TestLoopResizeUsesFullRowsWhenStatusBarMissing(t *testing.T) {
	t.Parallel()

	screen := newSimulationScreen(t, 20, 8)
	vt := &fakeVTSource{}
	loop, err := NewLoop(screen, vt, &DefaultTheme, nil)
	if err != nil {
		t.Fatalf("NewLoop returned error: %v", err)
	}

	if err := loop.Resize(20, 8); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	if vt.resizeRows != 8 {
		t.Fatalf("resize rows = %d, want 8 when status bar is nil", vt.resizeRows)
	}
}

func TestLoopResizeAllowsSingleRowWithoutStatusBar(t *testing.T) {
	t.Parallel()

	screen := newSimulationScreen(t, 20, 1)
	vt := &fakeVTSource{}
	loop, err := NewLoop(screen, vt, &DefaultTheme, nil)
	if err != nil {
		t.Fatalf("NewLoop returned error: %v", err)
	}

	if err := loop.Resize(20, 1); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	if vt.resizeRows != 1 {
		t.Fatalf("resize rows = %d, want 1 when status bar is nil and rows=1", vt.resizeRows)
	}
}

func TestLoopConcurrentAccessIsRaceFree(t *testing.T) {
	screen := newSimulationScreen(t, 40, 10)
	vt := &fakeVTSource{dirty: true}
	loop, err := NewLoop(screen, vt, &DefaultTheme, nil)
	if err != nil {
		t.Fatalf("NewLoop returned error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			loop.MarkDirty()
			loop.SetOverlayActive(i%2 == 0)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_, _ = loop.Frame()
		}
	}()

	wg.Wait()
}

type fakeVTSource struct {
	dirty      bool
	resizeCols int
	resizeRows int
}

func (f *fakeVTSource) CopyTo(_ Screen, _ int, _ int, _ int, _ int) error {
	return nil
}

func (f *fakeVTSource) Resize(cols int, rows int) error {
	f.resizeCols = cols
	f.resizeRows = rows
	f.dirty = true
	return nil
}

func (f *fakeVTSource) SetOverlayActive(_ bool) {}

func (f *fakeVTSource) Dirty() bool {
	return f.dirty
}

func (f *fakeVTSource) MarkClean() {
	f.dirty = false
}

type markerWidget struct {
	r rune
}

func (w *markerWidget) Paint(screen Screen, rect Rect, theme *Theme) {
	if screen == nil || theme == nil {
		return
	}

	screen.SetContent(rect.X, rect.Y, w.r, nil, theme.StatusFg)
}
