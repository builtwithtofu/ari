package tui

import (
	"os"
	"testing"

	"github.com/gdamore/tcell/v3"
)

func BenchmarkNoopFrame80x24(b *testing.B) {
	benchmarkNoopFrame(b, 80, 24)
}

func BenchmarkNoopFrame160x48(b *testing.B) {
	benchmarkNoopFrame(b, 160, 48)
}

func BenchmarkDirtyFrame160x48(b *testing.B) {
	screen := tcellBenchScreen(b, 160, 48)
	vt := &fakeVTSource{}
	loop, err := NewLoop(screen, vt, &DefaultTheme, nil)
	if err != nil {
		b.Fatalf("NewLoop returned error: %v", err)
	}
	loop.showFn = func() {}

	if _, err := loop.Frame(); err != nil {
		b.Fatalf("initial Frame returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vt.dirty = true
		if _, err := loop.Frame(); err != nil {
			b.Fatalf("Frame returned error: %v", err)
		}
	}
}

func BenchmarkCopyTo160x48(b *testing.B) {
	renderer, err := NewVTRenderer(160, 48)
	if err != nil {
		b.Fatalf("NewVTRenderer returned error: %v", err)
	}
	b.Cleanup(renderer.Close)

	if _, err := renderer.Write([]byte("copy benchmark\n")); err != nil {
		b.Fatalf("Write returned error: %v", err)
	}

	screen := tcellBenchScreen(b, 160, 48)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := renderer.CopyTo(screen, 0, 0, 160, 48); err != nil {
			b.Fatalf("CopyTo returned error: %v", err)
		}
	}
}

func BenchmarkFullFrame160x48(b *testing.B) {
	renderer, err := NewVTRenderer(160, 47)
	if err != nil {
		b.Fatalf("NewVTRenderer returned error: %v", err)
	}
	b.Cleanup(renderer.Close)

	screen := tcellBenchScreen(b, 160, 48)
	loop, err := NewLoop(screen, renderer, &DefaultTheme, nil)
	if err != nil {
		b.Fatalf("NewLoop returned error: %v", err)
	}
	loop.showFn = func() {}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := renderer.Write([]byte("frame\r")); err != nil {
			b.Fatalf("Write returned error: %v", err)
		}

		if _, err := loop.Frame(); err != nil {
			b.Fatalf("Frame returned error: %v", err)
		}
	}
}

func BenchmarkIntegrationRealScreen160x48(b *testing.B) {
	if os.Getenv("ARI_TUI_BENCH_REAL_SCREEN") != "1" {
		b.Skip("set ARI_TUI_BENCH_REAL_SCREEN=1 to run real-screen integration benchmark")
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		b.Skipf("real screen unavailable: %v", err)
	}

	if err := screen.Init(); err != nil {
		b.Skipf("real screen init failed: %v", err)
	}
	b.Cleanup(screen.Fini)

	screen.SetSize(160, 48)

	renderer, err := NewVTRenderer(160, 47)
	if err != nil {
		b.Fatalf("NewVTRenderer returned error: %v", err)
	}
	b.Cleanup(renderer.Close)

	loop, err := NewLoop(AdaptTCellScreen(screen), renderer, &DefaultTheme, nil)
	if err != nil {
		b.Fatalf("NewLoop returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := renderer.Write([]byte("integration\r")); err != nil {
			b.Fatalf("Write returned error: %v", err)
		}

		if _, err := loop.Frame(); err != nil {
			b.Fatalf("Frame returned error: %v", err)
		}
	}
}

func TestAllocGate(t *testing.T) {
	t.Parallel()

	result80x24 := testing.Benchmark(func(b *testing.B) {
		benchmarkNoopFrame(b, 80, 24)
	})
	if got := result80x24.AllocsPerOp(); got != 0 {
		t.Fatalf("no-op 80x24 allocs/op = %d, want 0", got)
	}

	result160x48 := testing.Benchmark(func(b *testing.B) {
		benchmarkNoopFrame(b, 160, 48)
	})
	if got := result160x48.AllocsPerOp(); got != 0 {
		t.Fatalf("no-op 160x48 allocs/op = %d, want 0", got)
	}
}

func benchmarkNoopFrame(b *testing.B, width int, height int) {
	b.Helper()

	screen := tcellBenchScreen(b, width, height)
	vt := &fakeVTSource{}
	loop, err := NewLoop(screen, vt, &DefaultTheme, nil)
	if err != nil {
		b.Fatalf("NewLoop returned error: %v", err)
	}
	loop.showFn = func() {}

	if _, err := loop.Frame(); err != nil {
		b.Fatalf("initial Frame returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loop.Frame(); err != nil {
			b.Fatalf("Frame returned error: %v", err)
		}
	}
}

func tcellBenchScreen(b *testing.B, width int, height int) *testScreen {
	b.Helper()

	screen := newTestScreen(width, height)
	if err := screen.Init(); err != nil {
		b.Fatalf("Init returned error: %v", err)
	}
	b.Cleanup(screen.Fini)

	screen.SetSize(width, height)
	return screen
}
