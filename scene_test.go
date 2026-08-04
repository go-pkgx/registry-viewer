// SPDX-License-Identifier: BSD-3-Clause
//
// scene_test — off-browser tests for the registry-viewer scene. main.go
// carries a js && wasm build tag so it drops out on the native test
// host; scene.go stays tagless so this file exercises it against a plain
// RGBA byte buffer rendered through painter.NewPixelPainter.
//
// The assertions are position-precise: they compute the exact surface
// rectangles the box layout produces (search box, filter-combo strip,
// TreeTable grid) and the exact y-offset of each grid BODY row
// (gridTop + header + index*rowHeight), then verify the SELECTED-row
// Accent band, the header band, the column separator and a dated
// Published cell all paint where the model says — tying the filtered
// model directly to real pixels. Filter tests assert the grid's visible
// package set shrinks to the exact expected packages.

package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

// scrollbarW mirrors the toolkit's unexported scroll.go scrollbarWidth (8):
// the grid is windowed (32 flattened rows > ~19 visible), so its body width
// is Bounds().W - scrollbarW.
const scrollbarW = 8

func newSurface() []byte { return make([]byte, 4*surfaceW*surfaceH) }

func px(surf []byte, x, y int) toolkit.RGBA {
	i := 4 * (y*surfaceW + x)
	return toolkit.RGBA{R: surf[i], G: surf[i+1], B: surf[i+2], A: surf[i+3]}
}

func eqColor(a, b toolkit.RGBA) bool { return a.R == b.R && a.G == b.G && a.B == b.B }

// gridGeom returns the grid's body width, the Package (col 0) width and the
// x of the column separator, computed from the layout the same way the
// TreeTable does (fixed Published column, auto Package column).
func gridGeom(s *state) (bodyW, col0W, sepX int) {
	r := s.grid.Bounds()
	bodyW = r.W - scrollbarW // windowed
	col0W = bodyW - gridPubColW
	sepX = r.X + col0W
	return
}

// hasInkIn reports whether any pixel in the [x0,x1)×[y0,y1) band differs
// from bg (i.e. some text/glyph was painted there).
func hasInkIn(surf []byte, x0, x1, y0, y1 int, bg toolkit.RGBA) bool {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !eqColor(px(surf, x, y), bg) {
				return true
			}
		}
	}
	return false
}

// --- construction ---------------------------------------------------------

func TestNewStateParsesEmbeddedSample(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	if s == nil {
		t.Fatal("newState returned nil")
	}
	if len(s.pkgs) != 14 {
		t.Fatalf("embedded sample rows = %d, want 14", len(s.pkgs))
	}
	if got := s.osDomain; !reflect.DeepEqual(got, []string{"darwin", "linux", "windows"}) {
		t.Fatalf("osDomain = %v, want [darwin linux windows]", got)
	}
	if got := s.archDomain; !reflect.DeepEqual(got, []string{"amd64", "arm64"}) {
		t.Fatalf("archDomain = %v, want [amd64 arm64]", got)
	}
	if s.search == nil || s.osDrop == nil || s.archDrop == nil || s.verDrop == nil || s.grid == nil || s.status == nil {
		t.Fatal("newState left a core widget nil")
	}
	// The grid has the two columns: Package (tree) + Published (date).
	if len(s.grid.Columns) != 2 || s.grid.Columns[0].Title != "Package" || s.grid.Columns[1].Title != "Published" {
		t.Fatalf("grid columns = %+v, want [Package][Published]", s.grid.Columns)
	}
	if s.grid.Columns[1].Width != gridPubColW {
		t.Fatalf("Published column width = %d, want %d", s.grid.Columns[1].Width, gridPubColW)
	}
	// Five distinct packages in the sample.
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"hugo", "jq", "lz4", "ripgrep", "zstd"}) {
		t.Fatalf("shownNames = %v, want [hugo jq lz4 ripgrep zstd]", got)
	}
}

func TestNewStateFallsBackOnBadJSON(t *testing.T) {
	s := newState(surfaceW, surfaceH, []byte("}{ not json"))
	if len(s.pkgs) != 14 {
		t.Fatalf("bad-JSON input should fall back to embedded sample; rows = %d", len(s.pkgs))
	}
}

func TestNewStateFallsBackOnEmptyArray(t *testing.T) {
	s := newState(surfaceW, surfaceH, []byte("[]"))
	if len(s.pkgs) != 14 {
		t.Fatalf("empty-array input should fall back to embedded sample; rows = %d", len(s.pkgs))
	}
}

// --- layout: exact rectangles --------------------------------------------

func TestLayoutRectsArePrecise(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)

	wantSearch := toolkit.Rect{X: margin, Y: margin, W: surfaceW - 2*margin, H: searchH}
	if got := s.search.Bounds(); got != wantSearch {
		t.Fatalf("search rect = %+v, want %+v", got, wantSearch)
	}

	// Filter strip sits below the search box: Y = margin + searchH + gap.
	wantStripY := margin + searchH + gap // 8 + 26 + 6 = 40
	if got := s.filterRow.Bounds(); got.Y != wantStripY || got.H != filterH || got.X != margin {
		t.Fatalf("filter strip rect = %+v, want Y=%d H=%d X=%d", got, wantStripY, filterH, margin)
	}
	// Three combos, left-to-right, inside the strip, same Y/H as the strip.
	os, ar, ve := s.osDrop.Bounds(), s.archDrop.Bounds(), s.verDrop.Bounds()
	if !(os.X < ar.X && ar.X < ve.X) {
		t.Fatalf("combos not left-to-right: os.X=%d arch.X=%d ver.X=%d", os.X, ar.X, ve.X)
	}
	if os.Y != wantStripY || ar.Y != wantStripY || ve.Y != wantStripY {
		t.Fatalf("combo Y mismatch: %d %d %d, want %d", os.Y, ar.Y, ve.Y, wantStripY)
	}

	// Grid: Y = margin + searchH + gap + filterH + gap; fills to the status bar.
	wantGridY := margin + searchH + gap + filterH + gap // 8+26+6+28+6 = 74
	wantGridH := (surfaceH - 2*margin) - (searchH + filterH + toolkit.StatusbarH) - 3*gap
	wantGrid := toolkit.Rect{X: margin, Y: wantGridY, W: surfaceW - 2*margin, H: wantGridH}
	if got := s.grid.Bounds(); got != wantGrid {
		t.Fatalf("grid rect = %+v, want %+v", got, wantGrid)
	}
}

// --- render: model row index -> exact pixel y ----------------------------

// TestVisibleRowsMatchDefaultGrid asserts the flattened visible order of the
// default (all-expanded) forest, giving each row a known body index and thus
// a known y-offset. The publish date lives in each version leaf's Published
// cell (not appended to the version text); the hugo/linux-amd64 0.128.0 leaf
// has an EMPTY date, exercising the blank-cell path.
func TestVisibleRowsMatchDefaultGrid(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	rows := s.visibleRows()
	// No synthetic root — package names are the top-level forest (depth 0).
	want := []visibleRow{
		{"hugo", "", 0},
		{"darwin/arm64", "", 1},
		{"0.129.0", "2026-08-03", 2},
		{"linux/amd64", "", 1},
		{"0.128.0", "", 2}, // empty published cell
		{"jq", "", 0},
	}
	if len(rows) < len(want) {
		t.Fatalf("only %d visible rows, want at least %d", len(rows), len(want))
	}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("visible row %d = %+v, want %+v", i, rows[i], w)
		}
	}
	// Row y math: body row i sits at gridTop + header + i*rowHeight.
	wantY := s.grid.Bounds().Y + toolkit.TreeTableHeaderHeight + 2*toolkit.TreeTableRowHeight
	if got := s.rowY(2); got != wantY {
		t.Fatalf("rowY(2) = %d, want %d", got, wantY)
	}
}

// TestVersionLeafCarriesPublishedDate walks the filtered grid and asserts a
// specific version leaf's Cells = [version, date], and that the empty-date
// leaf's Published cell is blank — the date threaded from the model into the
// TreeTable cells the WASM view paints.
func TestVersionLeafCarriesPublishedDate(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	var lz4 *toolkit.TreeTableNode
	for _, n := range s.grid.Root {
		if cellAt(n, 0) == "lz4" {
			lz4 = n
		}
	}
	if lz4 == nil {
		t.Fatal("lz4 node missing")
	}
	found := false
	for _, plat := range lz4.Children {
		if cellAt(plat, 0) != "linux/amd64" {
			continue
		}
		for _, leaf := range plat.Children {
			if cellAt(leaf, 0) == "1.10.0" {
				found = true
				if cellAt(leaf, 1) != "2026-08-02" {
					t.Fatalf("lz4 1.10.0 Published cell = %q, want 2026-08-02", cellAt(leaf, 1))
				}
			}
		}
	}
	if !found {
		t.Fatal("lz4 linux/amd64 1.10.0 leaf not found")
	}
	// hugo 0.128.0 leaf: empty Published cell.
	for _, n := range s.grid.Root {
		if cellAt(n, 0) != "hugo" {
			continue
		}
		for _, plat := range n.Children {
			for _, leaf := range plat.Children {
				if cellAt(leaf, 0) == "0.128.0" && cellAt(leaf, 1) != "" {
					t.Fatalf("undated hugo 0.128.0 Published cell = %q, want empty", cellAt(leaf, 1))
				}
			}
		}
	}
}

// TestGridHeaderAndColumnsPaint asserts the pixel-level grid chrome: the
// header band (SurfaceAlt) at the grid top, the column separator (Border) at
// the computed Package/Published boundary, and that a dated version leaf
// paints INK in its Published column while the undated leaf's Published
// column stays blank — proving the date renders in its own column, at the row
// the model predicts.
func TestGridHeaderAndColumnsPaint(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	theme := s.theme
	surf := newSurface()
	s.draw(surf)

	r := s.grid.Bounds()
	bodyW, col0W, sepX := gridGeom(s)
	_ = bodyW

	// Header band: SurfaceAlt across the header height (sample an empty spot in
	// the Package header, right of the "Package" title text).
	if got := px(surf, r.X+col0W-24, r.Y+3); !eqColor(got, theme.SurfaceAlt) {
		t.Fatalf("header band pixel = %+v, want SurfaceAlt %+v", got, theme.SurfaceAlt)
	}
	// Column separator: a 1px Border line at sepX spanning the grid height.
	if got := px(surf, sepX, r.Y+3); !eqColor(got, theme.Border) {
		t.Fatalf("column separator at x=%d = %+v, want Border %+v", sepX, got, theme.Border)
	}

	// Published column band, excluding the separator column and the scrollbar.
	pubX0, pubX1 := sepX+2, r.X+col0W+gridPubColW-1

	// Dated leaf (visible index 2 = hugo darwin/arm64 0.129.0, 2026-08-03):
	// its Published column must carry ink.
	datedY := s.rowY(2)
	if !hasInkIn(surf, pubX0, pubX1, datedY+2, datedY+toolkit.TreeTableRowHeight-2, theme.Surface) {
		t.Fatalf("dated row at y=%d shows NO ink in the Published column [%d,%d)", datedY, pubX0, pubX1)
	}
	// Undated leaf (visible index 4 = hugo linux/amd64 0.128.0, ""): its
	// Published column must be blank (only the row's Surface fill).
	undatedY := s.rowY(4)
	if hasInkIn(surf, pubX0, pubX1, undatedY+2, undatedY+toolkit.TreeTableRowHeight-2, theme.Surface) {
		t.Fatalf("undated row at y=%d unexpectedly shows ink in the Published column", undatedY)
	}
}

// TestSelectedRowPaintsAccentAtExpectedY selects the "hugo" row (body index
// 0), renders, and asserts the Accent selection band lands exactly at the
// grid's first body row — and that the header band and an unselected row do
// NOT carry the Accent fill. Binds the model's row index to a real pixel y.
func TestSelectedRowPaintsAccentAtExpectedY(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	theme := s.theme
	hugo := s.grid.Root[0]
	if cellAt(hugo, 0) != "hugo" {
		t.Fatalf("first package row = %q, want hugo", cellAt(hugo, 0))
	}
	s.grid.Selected = hugo

	surf := newSurface()
	s.draw(surf)

	gx := s.grid.Bounds().X
	// hugo row (body index 0): background must be Accent.
	hugoY := s.rowY(0)
	if got := px(surf, gx+1, hugoY+4); !eqColor(got, theme.Accent) {
		t.Fatalf("selected hugo row at y=%d: pixel %+v, want Accent %+v", hugoY, got, theme.Accent)
	}
	// Header band (above body): SurfaceAlt, never Accent.
	if got := px(surf, gx+1, s.grid.Bounds().Y+3); eqColor(got, theme.Accent) {
		t.Fatalf("header band unexpectedly Accent-filled: %+v", got)
	}
	// jq row (body index 5): unselected -> Surface, not Accent.
	jqY := s.rowY(5)
	if got := px(surf, gx+1, jqY+4); eqColor(got, theme.Accent) {
		t.Fatalf("jq row at y=%d unexpectedly Accent-filled: %+v", jqY, got)
	}
	if got := px(surf, gx+1, jqY+4); !eqColor(got, theme.Surface) {
		t.Fatalf("jq row at y=%d: pixel %+v, want Surface %+v", jqY, got, theme.Surface)
	}
}

func TestDrawFillsBackground(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	surf := newSurface()
	s.draw(surf)
	for i := 3; i+3 < len(surf); i += 4 {
		if surf[i] == 0 {
			t.Fatalf("draw left alpha 0 at byte %d — background fill missing", i)
		}
	}
}

// --- filters --------------------------------------------------------------

func TestNameFilterReducesToExpectedSet(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	if got := len(s.shownNames()); got != 5 {
		t.Fatalf("unfiltered package count = %d, want 5", got)
	}
	// "lz" substring -> only lz4.
	s.search.OnChange("lz")
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"lz4"}) {
		t.Fatalf("name filter 'lz' -> %v, want [lz4]", got)
	}
	// "z" substring -> lz4 + zstd (case-insensitive, sorted).
	s.search.OnChange("Z")
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"lz4", "zstd"}) {
		t.Fatalf("name filter 'Z' -> %v, want [lz4 zstd]", got)
	}
	// Clearing restores all five.
	s.search.OnChange("")
	if got := len(s.shownNames()); got != 5 {
		t.Fatalf("cleared name filter count = %d, want 5", got)
	}
}

func TestOSFilterNarrowsGrid(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// osDomain = [darwin linux windows]; "windows" is domain index 2 ->
	// DropDown option 3.
	s.osDrop.Select(3)
	if s.osFilter != "windows" {
		t.Fatalf("osFilter = %q, want windows", s.osFilter)
	}
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"ripgrep", "zstd"}) {
		t.Fatalf("os=windows -> %v, want [ripgrep zstd]", got)
	}
	// Back to All.
	s.osDrop.Select(0)
	if s.osFilter != "" {
		t.Fatalf("osFilter after All = %q, want empty", s.osFilter)
	}
	if got := len(s.shownNames()); got != 5 {
		t.Fatalf("os=All count = %d, want 5", got)
	}
}

func TestVersionFilterNarrowsGrid(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	idx := -1
	for i, v := range s.verDomain {
		if v == "1.10.0" {
			idx = i + 1 // +1 for the leading "All"
		}
	}
	if idx < 0 {
		t.Fatal("version 1.10.0 not in domain")
	}
	s.verDrop.Select(idx)
	if s.verFilter != "1.10.0" {
		t.Fatalf("verFilter = %q, want 1.10.0", s.verFilter)
	}
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"lz4"}) {
		t.Fatalf("version=1.10.0 -> %v, want [lz4]", got)
	}
}

// TestDropDownPopoverRouting opens the version combo via a real handleClick,
// renders the popover, selects an option through a popover click, and
// confirms an outside click dismisses it — exercising the shared host-owned
// popover draw + click routing (which serves all three combos).
func TestDropDownPopoverRouting(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	dr := s.verDrop.Bounds()
	s.handleClick(dr.X+dr.W/2, dr.Y+dr.H/2)
	if !s.verDrop.Open {
		t.Fatal("clicking the version combo should open its popover")
	}
	s.draw(newSurface()) // renders the popover branch (must not panic)

	pb := s.verDrop.PopoverBounds()
	// Option row 1 is the first real version (row 0 is "All").
	s.handleClick(pb.X+5, pb.Y+dropDownRowH+dropDownRowH/2)
	if s.verDrop.Open {
		t.Fatal("selecting a popover option should close it")
	}
	if s.verFilter != s.verDomain[0] {
		t.Fatalf("verFilter = %q, want %q", s.verFilter, s.verDomain[0])
	}
	// Reopen, then dismiss with an outside click.
	s.handleClick(dr.X+dr.W/2, dr.Y+dr.H/2)
	if !s.verDrop.Open {
		t.Fatal("version combo should reopen")
	}
	s.handleClick(1, 1) // top-left, well outside the popover
	if s.verDrop.Open {
		t.Fatal("outside click should dismiss the popover")
	}
}

func TestCombinedFiltersAND(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	s.search.OnChange("lz4")
	// archDomain = [amd64 arm64]; "arm64" -> domain index 1 -> option 2.
	s.archDrop.Select(2)
	if s.archFilter != "arm64" {
		t.Fatalf("archFilter = %q, want arm64", s.archFilter)
	}
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"lz4"}) {
		t.Fatalf("lz4 + arm64 -> %v, want [lz4]", got)
	}
	// Under lz4, only arm64 os/arch groups should remain (darwin/arm64,
	// linux/arm64); no amd64 group survives the AND.
	for _, r := range s.visibleRows() {
		if r.Depth == 1 && r.Label == "linux/amd64" {
			t.Fatalf("amd64 group leaked past the arm64 filter: %+v", r)
		}
	}
}

func TestStatusCountUpdates(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	if s.status.Segments[0] != "5 packages" || s.status.Segments[1] != "5 shown" {
		t.Fatalf("initial status = %v, want [5 packages][5 shown]", s.status.Segments[:2])
	}
	s.search.OnChange("lz")
	if s.status.Segments[0] != "5 packages" || s.status.Segments[1] != "1 shown" {
		t.Fatalf("filtered status = %v, want [5 packages][1 shown]", s.status.Segments[:2])
	}
}

// --- event routing --------------------------------------------------------

func TestClickSearchFocusesAndTypes(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	r := s.search.Bounds()
	s.handleClick(r.X+30, r.Y+r.H/2)
	if !s.search.Focused || s.keyTarget != toolkit.Widget(s.search) {
		t.Fatal("clicking the search box should focus it for keyboard input")
	}
	// Type "l" then "z" -> live name filter "lz" -> only lz4.
	if !s.handleChar("l") || !s.handleChar("z") {
		t.Fatal("handleChar should consume input while the search is focused")
	}
	if s.nameFilter != "lz" {
		t.Fatalf("nameFilter after typing = %q, want lz", s.nameFilter)
	}
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"lz4"}) {
		t.Fatalf("typed filter -> %v, want [lz4]", got)
	}
	// Backspace removes the 'z'.
	if !s.handleKeyDown("Backspace") {
		t.Fatal("handleKeyDown should consume Backspace")
	}
	if s.nameFilter != "l" {
		t.Fatalf("nameFilter after Backspace = %q, want l", s.nameFilter)
	}
}

func TestClickClearAffordanceResetsFilter(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Type into the search box so both SearchEntry.Text and nameFilter are set
	// (the clear affordance is a no-op when Text is empty).
	r := s.search.Bounds()
	s.handleClick(r.X+30, r.Y+r.H/2)
	for _, ch := range []string{"l", "z", "4"} {
		s.handleChar(ch)
	}
	if s.search.Text != "lz4" || s.nameFilter != "lz4" {
		t.Fatalf("precondition: Text=%q nameFilter=%q, want lz4", s.search.Text, s.nameFilter)
	}
	// Click the trailing clear slot: local x in [W-pad-iconW, W-pad).
	clearX := r.X + r.W - toolkit.SearchEntryPadX - toolkit.SearchEntryIconW/2
	s.handleClick(clearX, r.Y+r.H/2)
	if s.search.Text != "" || s.nameFilter != "" {
		t.Fatalf("clear slot click should reset filter; Text=%q nameFilter=%q", s.search.Text, s.nameFilter)
	}
	if got := len(s.shownNames()); got != 5 {
		t.Fatalf("count after clear = %d, want 5", got)
	}
}

func TestClickComboMovesFocusOffSearch(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Focus the search first.
	sr := s.search.Bounds()
	s.handleClick(sr.X+30, sr.Y+sr.H/2)
	if !s.search.Focused {
		t.Fatal("precondition: search should be focused")
	}
	// Click the os combo: it opens and takes focus off the search.
	r := s.osDrop.Bounds()
	s.handleClick(r.X+r.W/2, r.Y+r.H/2)
	if !s.osDrop.Open {
		t.Fatal("clicking the os combo should open it")
	}
	if s.keyTarget != toolkit.Widget(s.osDrop) || s.search.Focused {
		t.Fatal("clicking a combo should move keyboard focus off the search box")
	}
}

func TestClickGridSelectsRow(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Click the first body row (body index 0 = hugo) past the chevron.
	y := s.rowY(0) + toolkit.TreeTableRowHeight/2
	s.handleClick(s.grid.Bounds().X+toolkit.TreeChevronW+30, y)
	if s.grid.Selected == nil || cellAt(s.grid.Selected, 0) != "hugo" {
		t.Fatalf("clicking the first grid row should select hugo; Selected=%v", s.grid.Selected)
	}
}

func TestClickDeadSpaceClearsFocus(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Focus the search first.
	sr := s.search.Bounds()
	s.handleClick(sr.X+30, sr.Y+sr.H/2)
	if !s.search.Focused {
		t.Fatal("precondition: search should be focused")
	}
	// Click far below every widget (in the status bar region).
	s.handleClick(surfaceW/2, surfaceH-2)
	if s.keyTarget != nil || s.search.Focused {
		t.Fatal("dead-space click should clear keyboard focus")
	}
}

func TestNoTargetKeyHandlersAreNoOps(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	s.keyTarget = nil
	if s.handleChar("x") || s.handleKeyDown("Backspace") {
		t.Fatal("key handlers with no focus target should report no change")
	}
}

func TestHandleMoveAndReleaseAreNoOps(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	if s.handleMove(10, 10) || s.handleRelease(10, 10) {
		t.Fatal("move/release should be no-ops in this scene")
	}
}

// gridWindow returns the number of BODY rows the grid can show at once,
// computed the same way the toolkit's TreeTable.bodyVisibleRows does
// (bounds height minus the header, divided by the row height).
func gridWindow(s *state) int {
	return (s.grid.Bounds().H - toolkit.TreeTableHeaderHeight) / toolkit.TreeTableRowHeight
}

// gridPoint returns a surface point that lands inside the grid's body, so a
// forwarded EventScroll routes (through the root VBox) to the grid.
func gridPoint(s *state) (int, int) {
	r := s.grid.Bounds()
	return r.X + 20, r.Y + toolkit.TreeTableHeaderHeight + toolkit.TreeTableRowHeight/2
}

// TestWheelScrollShiftsGridWindow forwards EventScroll through handleScroll
// and asserts the grid's visible window shifts: the first drawn body row's
// model index (grid.ScrollRow) increases on a down-scroll, and both ends
// CLAMP (up past the top pins at 0, down past the end pins at total-window).
// The app owns no scroll math — the TreeTable clamps itself.
func TestWheelScrollShiftsGridWindow(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)

	// Scrolling is only meaningful when there are more rows than fit.
	total := len(s.visibleRows())
	window := gridWindow(s)
	if total <= window {
		t.Fatalf("need more rows than fit for a scroll test: total=%d window=%d", total, window)
	}
	maxScroll := total - window

	if s.grid.ScrollRow != 0 {
		t.Fatalf("fresh grid ScrollRow = %d, want 0", s.grid.ScrollRow)
	}

	x, y := gridPoint(s)

	// Down-scroll: the first drawn body row's model index must increase.
	before := s.grid.ScrollRow
	if !s.handleScroll(x, y, 3) {
		t.Fatal("handleScroll should report a change (re-render)")
	}
	after := s.grid.ScrollRow
	if after <= before {
		t.Fatalf("down-scroll: first body row index %d did not increase past %d", after, before)
	}
	if after != 3 {
		t.Fatalf("down-scroll by 3 rows: ScrollRow = %d, want 3", after)
	}

	// Down past the end clamps at total-window (last full window), not beyond.
	s.handleScroll(x, y, 10*total)
	if s.grid.ScrollRow != maxScroll {
		t.Fatalf("over-scroll down: ScrollRow = %d, want clamp at %d", s.grid.ScrollRow, maxScroll)
	}

	// Up past the top clamps at 0 (no negative offset).
	s.handleScroll(x, y, -10*total)
	if s.grid.ScrollRow != 0 {
		t.Fatalf("over-scroll up: ScrollRow = %d, want clamp at 0", s.grid.ScrollRow)
	}

	// A scroll whose point falls on a non-scrollable widget (the search box)
	// routes there and leaves the grid untouched — still reports a re-render.
	sr := s.search.Bounds()
	if !s.handleScroll(sr.X+5, sr.Y+sr.H/2, 3) {
		t.Fatal("handleScroll always reports true")
	}
	if s.grid.ScrollRow != 0 {
		t.Fatalf("scroll over the search box moved the grid: ScrollRow = %d, want 0", s.grid.ScrollRow)
	}
}

// TestScrolledRenderDiffersAndDumpsPNG renders the grid at ScrollRow 0 and
// again after a wheel down-scroll, asserts the first body row's pixels
// actually changed (the grid really repainted a different top row), and
// writes testdata/render_scrolled.png for visual inspection.
func TestScrolledRenderDiffersAndDumpsPNG(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)

	// Unscrolled top-of-body capture.
	surf0 := newSurface()
	s.draw(surf0)

	x, y := gridPoint(s)
	s.handleScroll(x, y, 6) // scroll down six rows
	if s.grid.ScrollRow == 0 {
		t.Fatal("precondition: grid should have scrolled off row 0")
	}

	surf1 := newSurface()
	s.draw(surf1)

	// The first body row band must differ between the two renders: a scrolled
	// grid paints a different model row at the top.
	r := s.grid.Bounds()
	y0 := r.Y + toolkit.TreeTableHeaderHeight + 2
	y1 := y0 + toolkit.TreeTableRowHeight - 4
	x0, x1 := r.X+2, r.X+r.W-scrollbarW-2
	differs := false
	for yy := y0; yy < y1 && !differs; yy++ {
		for xx := x0; xx < x1; xx++ {
			if !eqColor(px(surf0, xx, yy), px(surf1, xx, yy)) {
				differs = true
				break
			}
		}
	}
	if !differs {
		t.Fatal("scrolled render's top body row is pixel-identical to the unscrolled one")
	}

	img := image.NewRGBA(image.Rect(0, 0, surfaceW, surfaceH))
	for yy := 0; yy < surfaceH; yy++ {
		for xx := 0; xx < surfaceW; xx++ {
			i := 4 * (yy*surfaceW + xx)
			img.Set(xx, yy, color.RGBA{surf1[i], surf1[i+1], surf1[i+2], surf1[i+3]})
		}
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	out := filepath.Join("testdata", "render_scrolled.png")
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("create %s: %v", out, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", out, err)
	}
	if img.Bounds().Dx() != surfaceW || img.Bounds().Dy() != surfaceH {
		t.Fatalf("render_scrolled.png dims = %dx%d, want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), surfaceW, surfaceH)
	}
}

// --- helpers + edge branches ---------------------------------------------

func TestDomainValueEdges(t *testing.T) {
	d := []string{"a", "b"}
	if domainValue(d, 0) != "" {
		t.Fatal("index 0 (All) should map to empty")
	}
	if domainValue(d, 3) != "" {
		t.Fatal("out-of-range index should map to empty")
	}
	if domainValue(d, 1) != "a" || domainValue(d, 2) != "b" {
		t.Fatal("valid indices should map to domain[i-1]")
	}
}

func TestCellAtShortRow(t *testing.T) {
	n := &toolkit.TreeTableNode{Cells: []string{"only-one"}}
	if cellAt(n, 0) != "only-one" {
		t.Fatal("cellAt(0) should return the present cell")
	}
	if cellAt(n, 1) != "" {
		t.Fatal("cellAt past the end should return empty (short-row tolerance)")
	}
}

func TestShownNamesAndVisibleRowsEmptyForest(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	s.grid.Root = nil
	if got := s.shownNames(); len(got) != 0 {
		t.Fatalf("shownNames with empty forest = %v, want empty", got)
	}
	if s.visibleRows() != nil {
		t.Fatal("visibleRows with empty forest should be nil")
	}
}

func TestParseRegistryError(t *testing.T) {
	if _, err := parseRegistry([]byte("nonsense")); err == nil {
		t.Fatal("parseRegistry should error on malformed JSON")
	}
	// A row WITH published, and one WITHOUT the key (tolerated -> "").
	rows, err := parseRegistry([]byte(`[
		{"name":"x","os":"linux","arch":"amd64","version":"1","published":"2026-08-02"},
		{"name":"y","os":"darwin","arch":"arm64","version":"2"}
	]`))
	if err != nil || len(rows) != 2 || rows[0].Name != "x" {
		t.Fatalf("parseRegistry valid input: rows=%v err=%v", rows, err)
	}
	if rows[0].Published != "2026-08-02" {
		t.Fatalf("row 0 Published = %q, want 2026-08-02", rows[0].Published)
	}
	if rows[1].Published != "" {
		t.Fatalf("row 1 (no published key) Published = %q, want empty", rows[1].Published)
	}
}

func TestBannerAndMain(t *testing.T) {
	if banner() == "" {
		t.Fatal("banner should be non-empty")
	}
	main() // native stub: prints the banner, must not panic
}

func TestInsideAndLocalHelpers(t *testing.T) {
	r := toolkit.Rect{X: 10, Y: 20, W: 30, H: 40}
	if !inside(15, 25, r) || inside(0, 0, r) || inside(40, 60, r) {
		t.Fatal("inside half-open containment wrong")
	}
	ev := local(toolkit.Event{X: 25, Y: 30}, r)
	if ev.X != 15 || ev.Y != 10 {
		t.Fatalf("local rebasing wrong: %+v", ev)
	}
}

// --- PNG dump (visual verification hook) ----------------------------------

// TestRenderDumpsPNG renders the default scene and writes testdata/render.png
// so the grid + columns + dates are inspectable, asserting it is a non-trivial
// image of the expected dimensions. When GO_PKGX_DUMP_PNG names a directory
// the PNG lands there too.
func TestRenderDumpsPNG(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Select the first row so the render also shows the Accent selection band.
	s.grid.Selected = s.grid.Root[0]
	surf := newSurface()
	s.draw(surf)

	img := image.NewRGBA(image.Rect(0, 0, surfaceW, surfaceH))
	for y := 0; y < surfaceH; y++ {
		for x := 0; x < surfaceW; x++ {
			i := 4 * (y*surfaceW + x)
			img.Set(x, y, color.RGBA{surf[i], surf[i+1], surf[i+2], surf[i+3]})
		}
	}

	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	writePNG := func(path string) {
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatalf("encode %s: %v", path, err)
		}
	}
	out := filepath.Join("testdata", "render.png")
	writePNG(out)
	if dir := os.Getenv("GO_PKGX_DUMP_PNG"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir dump dir: %v", err)
		}
		writePNG(filepath.Join(dir, "render.png"))
	}

	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat render.png: %v", err)
	}
	if fi.Size() < 1024 {
		t.Fatalf("render.png suspiciously small: %d bytes", fi.Size())
	}
	if img.Bounds().Dx() != surfaceW || img.Bounds().Dy() != surfaceH {
		t.Fatalf("render.png dims = %dx%d, want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), surfaceW, surfaceH)
	}
}
