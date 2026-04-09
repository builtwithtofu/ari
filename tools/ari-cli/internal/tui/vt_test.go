package tui

import (
	"sync"
	"testing"

	"github.com/gdamore/tcell/v3"
)

func TestVTRendererCopyToRendersANSIText(t *testing.T) {
	t.Parallel()

	renderer, err := NewVTRenderer(8, 2)
	if err != nil {
		t.Fatalf("NewVTRenderer returned error: %v", err)
	}
	t.Cleanup(renderer.Close)

	if _, err := renderer.Write([]byte("\x1b[31mA\x1b[0mB")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	screen := newSimulationScreen(t, 8, 2)
	if err := renderer.CopyTo(screen, 0, 0, 8, 2); err != nil {
		t.Fatalf("CopyTo returned error: %v", err)
	}

	if got := simulationRune(t, screen, 0, 0); got != 'A' {
		t.Fatalf("rune at (0,0) = %q, want %q", string(got), "A")
	}

	if got := simulationRune(t, screen, 1, 0); got != 'B' {
		t.Fatalf("rune at (1,0) = %q, want %q", string(got), "B")
	}

	if got := simulationForeground(t, screen, 0, 0); got != tcell.PaletteColor(1) {
		t.Fatalf("foreground at (0,0) = %v, want %v", got, tcell.PaletteColor(1))
	}
}

func TestVTRendererResizeAndCopyTo(t *testing.T) {
	t.Parallel()

	renderer, err := NewVTRenderer(8, 2)
	if err != nil {
		t.Fatalf("NewVTRenderer returned error: %v", err)
	}
	t.Cleanup(renderer.Close)

	if err := renderer.Resize(3, 1); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	if _, err := renderer.Write([]byte("abc")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	screen := newSimulationScreen(t, 3, 1)
	if err := renderer.CopyTo(screen, 0, 0, 3, 1); err != nil {
		t.Fatalf("CopyTo returned error: %v", err)
	}

	if got := simulationRune(t, screen, 0, 0); got != 'a' {
		t.Fatalf("rune at (0,0) = %q, want %q", string(got), "a")
	}

	if got := simulationRune(t, screen, 2, 0); got != 'c' {
		t.Fatalf("rune at (2,0) = %q, want %q", string(got), "c")
	}
}

func TestVTRendererOverlayPauseResumePreservesOutput(t *testing.T) {
	t.Parallel()

	renderer, err := NewVTRenderer(8, 1)
	if err != nil {
		t.Fatalf("NewVTRenderer returned error: %v", err)
	}
	t.Cleanup(renderer.Close)

	screen := newSimulationScreen(t, 8, 1)

	if _, err := renderer.Write([]byte("abc")); err != nil {
		t.Fatalf("first Write returned error: %v", err)
	}
	if err := renderer.CopyTo(screen, 0, 0, 8, 1); err != nil {
		t.Fatalf("initial CopyTo returned error: %v", err)
	}

	renderer.SetOverlayActive(true)
	if _, err := renderer.Write([]byte("\rXYZ")); err != nil {
		t.Fatalf("overlay Write returned error: %v", err)
	}
	if err := renderer.CopyTo(screen, 0, 0, 8, 1); err != nil {
		t.Fatalf("overlay CopyTo returned error: %v", err)
	}

	if got := simulationRune(t, screen, 0, 0); got != 'a' {
		t.Fatalf("rune at (0,0) during overlay = %q, want %q", string(got), "a")
	}

	renderer.SetOverlayActive(false)
	if err := renderer.CopyTo(screen, 0, 0, 8, 1); err != nil {
		t.Fatalf("resume CopyTo returned error: %v", err)
	}

	if got := simulationRune(t, screen, 0, 0); got != 'X' {
		t.Fatalf("rune at (0,0) after overlay = %q, want %q", string(got), "X")
	}

	if got := simulationRune(t, screen, 2, 0); got != 'Z' {
		t.Fatalf("rune at (2,0) after overlay = %q, want %q", string(got), "Z")
	}
}

func TestVTRendererCopyToUsesViewportOnScroll(t *testing.T) {
	t.Parallel()

	renderer, err := NewVTRenderer(8, 2)
	if err != nil {
		t.Fatalf("NewVTRenderer returned error: %v", err)
	}
	t.Cleanup(renderer.Close)

	if _, err := renderer.Write([]byte("line1\nline2\nline3\nline4")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	buf := renderer.term.Buffer()
	if buf.YDisp == 0 {
		t.Fatalf("buffer YDisp = %d, want > 0 for viewport scroll test", buf.YDisp)
	}

	screen := newSimulationScreen(t, 8, 2)
	if err := renderer.CopyTo(screen, 0, 0, 8, 2); err != nil {
		t.Fatalf("CopyTo returned error: %v", err)
	}

	expectedTop := buf.TranslateBufferLineToString(buf.YDisp, false, 0, 8)
	gotTop := simulationRow(screen, 0, 8)
	if gotTop != expectedTop {
		t.Fatalf("top row = %q, want %q", gotTop, expectedTop)
	}

	expectedBottom := buf.TranslateBufferLineToString(buf.YDisp+1, false, 0, 8)
	gotBottom := simulationRow(screen, 1, 8)
	if gotBottom != expectedBottom {
		t.Fatalf("bottom row = %q, want %q", gotBottom, expectedBottom)
	}
}

func TestVTRendererCopyToPreservesWideRuneCell(t *testing.T) {
	t.Parallel()

	renderer, err := NewVTRenderer(4, 1)
	if err != nil {
		t.Fatalf("NewVTRenderer returned error: %v", err)
	}
	t.Cleanup(renderer.Close)

	if _, err := renderer.Write([]byte("ab")); err != nil {
		t.Fatalf("seed Write returned error: %v", err)
	}

	screen := newSimulationScreen(t, 4, 1)
	if err := renderer.CopyTo(screen, 0, 0, 4, 1); err != nil {
		t.Fatalf("seed CopyTo returned error: %v", err)
	}

	if _, err := renderer.Write([]byte("\r世")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if err := renderer.CopyTo(screen, 0, 0, 4, 1); err != nil {
		t.Fatalf("CopyTo returned error: %v", err)
	}

	if got := simulationRune(t, screen, 0, 0); got != '世' {
		t.Fatalf("lead rune at (0,0) = %q, want %q", string(got), "世")
	}

	if got := simulationRune(t, screen, 1, 0); got != 0 {
		t.Fatalf("continuation cell at (1,0) = %q, want zero-width continuation", string(got))
	}
}

func TestVTRendererCopyToClearsRightSideAfterNarrowResize(t *testing.T) {
	t.Parallel()

	renderer, err := NewVTRenderer(8, 1)
	if err != nil {
		t.Fatalf("NewVTRenderer returned error: %v", err)
	}
	t.Cleanup(renderer.Close)

	if _, err := renderer.Write([]byte("abcdefgh")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	screen := newSimulationScreen(t, 8, 1)
	if err := renderer.CopyTo(screen, 0, 0, 8, 1); err != nil {
		t.Fatalf("initial CopyTo returned error: %v", err)
	}

	if err := renderer.Resize(4, 1); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}
	if _, err := renderer.Write([]byte("\r1234")); err != nil {
		t.Fatalf("Write after resize returned error: %v", err)
	}

	if err := renderer.CopyTo(screen, 0, 0, 8, 1); err != nil {
		t.Fatalf("CopyTo after resize returned error: %v", err)
	}

	for x := 4; x < 8; x++ {
		if got := simulationRune(t, screen, x, 0); got != ' ' {
			t.Fatalf("stale rune at (%d,0) = %q, want space", x, string(got))
		}
	}
}

func TestVTRendererConcurrentWriteAndCopyIsRaceFree(t *testing.T) {
	renderer, err := NewVTRenderer(80, 20)
	if err != nil {
		t.Fatalf("NewVTRenderer returned error: %v", err)
	}
	t.Cleanup(renderer.Close)

	screen := newSimulationScreen(t, 80, 20)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_, _ = renderer.Write([]byte("line\n"))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = renderer.CopyTo(screen, 0, 0, 80, 20)
			_ = renderer.Dirty()
			renderer.MarkClean()
		}
	}()

	wg.Wait()
}

func newSimulationScreen(t *testing.T, width int, height int) *testScreen {
	t.Helper()

	screen := newTestScreen(width, height)
	if err := screen.Init(); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(width, height)
	return screen
}

func simulationRune(t *testing.T, screen *testScreen, x int, y int) rune {
	t.Helper()

	return screen.cellAt(x, y).primary
}

func simulationForeground(t *testing.T, screen *testScreen, x int, y int) tcell.Color {
	t.Helper()

	return screen.cellAt(x, y).style.GetForeground()
}

func simulationRow(screen *testScreen, y int, width int) string {
	runes := make([]rune, width)
	for x := 0; x < width; x++ {
		r := screen.cellAt(x, y).primary
		if r == 0 {
			r = ' '
		}
		runes[x] = r
	}

	return string(runes)
}
