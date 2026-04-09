package tui

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v3"
	xterm "github.com/gitpod-io/xterm-go"
)

// VTRenderer bridges xterm-go's VT buffer into a tcell screen.
//
// The renderer owns one xterm terminal instance and exposes a small API for
// attach loops: write PTY bytes, resize the viewport, and copy visible cells
// into tcell's screen with SetContent.
type VTRenderer struct {
	mu             sync.Mutex
	term           *xterm.Terminal
	cols           int
	rows           int
	overlay        bool
	dirty          bool
	cellScratch    xterm.CellData
	lineScratch    []rune
	combineScratch []rune
}

func NewVTRenderer(cols int, rows int) (*VTRenderer, error) {
	if cols <= 0 {
		return nil, fmt.Errorf("cols must be greater than zero")
	}

	if rows <= 0 {
		return nil, fmt.Errorf("rows must be greater than zero")
	}

	term := xterm.New(
		xterm.WithCols(cols),
		xterm.WithRows(rows),
	)
	if term == nil {
		return nil, fmt.Errorf("xterm terminal is required")
	}

	return &VTRenderer{
		term:  term,
		cols:  cols,
		rows:  rows,
		dirty: true,
	}, nil
}

func (r *VTRenderer) Close() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.term == nil {
		return
	}

	r.term.Dispose()
}

func (r *VTRenderer) Write(data []byte) (int, error) {
	if r == nil {
		return 0, fmt.Errorf("renderer is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.term == nil {
		return 0, fmt.Errorf("renderer terminal is required")
	}

	n, err := r.term.Write(data)
	if err != nil {
		return n, err
	}

	if n > 0 {
		r.dirty = true
	}

	return n, nil
}

func (r *VTRenderer) Resize(cols int, rows int) error {
	if r == nil {
		return fmt.Errorf("renderer is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if cols <= 0 {
		return fmt.Errorf("cols must be greater than zero")
	}

	if rows <= 0 {
		return fmt.Errorf("rows must be greater than zero")
	}

	if r.term == nil {
		return fmt.Errorf("renderer terminal is required")
	}

	r.term.Resize(cols, rows)
	r.cols = cols
	r.rows = rows
	r.dirty = true

	return nil
}

func (r *VTRenderer) SetOverlayActive(active bool) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.overlay == active {
		return
	}

	r.overlay = active
	if !active {
		r.dirty = true
	}
}

func (r *VTRenderer) Dirty() bool {
	if r == nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.dirty
}

func (r *VTRenderer) MarkClean() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.dirty = false
}

func (r *VTRenderer) CopyTo(screen Screen, x int, y int, width int, height int) error {
	if r == nil {
		return fmt.Errorf("renderer is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if screen == nil {
		return fmt.Errorf("screen is required")
	}

	if width < 0 {
		return fmt.Errorf("width must be non-negative")
	}

	if height < 0 {
		return fmt.Errorf("height must be non-negative")
	}

	if r.overlay {
		return nil
	}

	if r.term == nil {
		return fmt.Errorf("renderer terminal is required")
	}

	visibleRows := height
	if visibleRows > r.rows {
		visibleRows = r.rows
	}

	visibleCols := width
	if visibleCols > r.cols {
		visibleCols = r.cols
	}

	buf := r.term.Buffer()
	if buf == nil {
		return fmt.Errorf("terminal buffer is required")
	}

	for row := 0; row < visibleRows; row++ {
		lineIndex := buf.YDisp + row
		line := buf.Lines.Get(lineIndex)
		if line == nil {
			for col := 0; col < width; col++ {
				screen.SetContent(x+col, y+row, ' ', nil, tcell.StyleDefault)
			}
			continue
		}

		for col := 0; col < visibleCols; col++ {
			line.LoadCell(col, &r.cellScratch)
			cellWidth := r.cellScratch.GetWidth()
			if cellWidth == 0 {
				continue
			}

			mainRune, combine := r.cellRunes()
			style := styleFromCellData(&r.cellScratch)
			screen.SetContent(x+col, y+row, mainRune, combine, style)

			if cellWidth == 2 && col+1 < width {
				screen.SetContent(x+col+1, y+row, 0, nil, style)
				col++
			}
		}

		for col := visibleCols; col < width; col++ {
			screen.SetContent(x+col, y+row, ' ', nil, tcell.StyleDefault)
		}
	}

	for row := visibleRows; row < height; row++ {
		for col := 0; col < width; col++ {
			screen.SetContent(x+col, y+row, ' ', nil, tcell.StyleDefault)
		}
	}

	return nil
}

func (r *VTRenderer) cellRunes() (rune, []rune) {
	r.lineScratch = append(r.lineScratch[:0], []rune(r.cellScratch.GetChars())...)
	if len(r.lineScratch) == 0 {
		return ' ', nil
	}

	mainRune := r.lineScratch[0]
	if len(r.lineScratch) == 1 {
		return mainRune, nil
	}

	r.combineScratch = append(r.combineScratch[:0], r.lineScratch[1:]...)
	return mainRune, r.combineScratch
}

func styleFromCellData(cell *xterm.CellData) tcell.Style {
	if cell == nil {
		return tcell.StyleDefault
	}

	fg := tcell.ColorDefault
	if cell.IsFgPalette() {
		fg = tcell.PaletteColor(cell.GetFgColor())
	}
	if cell.IsFgRGB() {
		fg = rgbColorFromPacked(cell.GetFgColor())
	}

	bg := tcell.ColorDefault
	if cell.IsBgPalette() {
		bg = tcell.PaletteColor(cell.GetBgColor())
	}
	if cell.IsBgRGB() {
		bg = rgbColorFromPacked(cell.GetBgColor())
	}

	if cell.IsInverse() != 0 {
		fg, bg = bg, fg
	}

	style := tcell.StyleDefault.Foreground(fg).Background(bg)
	if cell.IsBold() != 0 {
		style = style.Bold(true)
	}
	if cell.IsDim() != 0 {
		style = style.Dim(true)
	}
	if cell.IsItalic() != 0 {
		style = style.Italic(true)
	}
	if cell.IsUnderline() != 0 {
		style = style.Underline(true)
	}
	if cell.IsBlink() != 0 {
		style = style.Blink(true)
	}
	if cell.IsStrikethrough() != 0 {
		style = style.StrikeThrough(true)
	}
	if cell.IsInvisible() != 0 {
		style = style.Foreground(bg)
	}

	return style
}

func rgbColorFromPacked(v int) tcell.Color {
	r := int32((v >> 16) & 0xff)
	g := int32((v >> 8) & 0xff)
	b := int32(v & 0xff)

	return tcell.NewRGBColor(r, g, b)
}
