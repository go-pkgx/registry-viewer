// SPDX-License-Identifier: BSD-3-Clause
//
// scene_test — off-browser tests for the registry-viewer scene. main.go
// carries a js && wasm build tag so it drops out on the native test
// host; scene.go stays tagless so this file exercises it against a plain
// RGBA byte buffer rendered through painter.NewPixelPainter.
//
// The assertions are position-precise: they compute the exact surface
// rectangles the box layout produces (search box, filter strip, tree)
// and the exact y-offset of each tree row (treeTop + index*rowHeight),
// then verify the SELECTED-row Accent band paints at that y — tying the
// filtered model directly to real pixels. Filter tests assert the tree's
// visible package-name set shrinks to the exact expected packages.

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

func newSurface() []byte { return make([]byte, 4*surfaceW*surfaceH) }

func px(surf []byte, x, y int) toolkit.RGBA {
	i := 4 * (y*surfaceW + x)
	return toolkit.RGBA{R: surf[i], G: surf[i+1], B: surf[i+2], A: surf[i+3]}
}

func eqColor(a, b toolkit.RGBA) bool { return a.R == b.R && a.G == b.G && a.B == b.B }

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
	if s.search == nil || s.osSwitch == nil || s.archSwitch == nil || s.verDrop == nil || s.tree == nil || s.status == nil {
		t.Fatal("newState left a core widget nil")
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
	if got := s.filterRow.Bounds(); got.Y != wantStripY || got.H != switchH || got.X != margin {
		t.Fatalf("filter strip rect = %+v, want Y=%d H=%d X=%d", got, wantStripY, switchH, margin)
	}
	// Three switchers, left-to-right, inside the strip, same Y/H as the strip.
	os, ar, ve := s.osSwitch.Bounds(), s.archSwitch.Bounds(), s.verDrop.Bounds()
	if !(os.X < ar.X && ar.X < ve.X) {
		t.Fatalf("switchers not left-to-right: os.X=%d arch.X=%d ver.X=%d", os.X, ar.X, ve.X)
	}
	if os.Y != wantStripY || ar.Y != wantStripY || ve.Y != wantStripY {
		t.Fatalf("switcher Y mismatch: %d %d %d, want %d", os.Y, ar.Y, ve.Y, wantStripY)
	}

	// Tree: Y = margin + searchH + gap + switchH + gap; fills to the status bar.
	wantTreeY := margin + searchH + gap + switchH + gap // 8+26+6+28+6 = 74
	wantTreeH := (surfaceH - 2*margin) - (searchH + switchH + toolkit.StatusbarH) - 3*gap
	wantTree := toolkit.Rect{X: margin, Y: wantTreeY, W: surfaceW - 2*margin, H: wantTreeH}
	if got := s.tree.Bounds(); got != wantTree {
		t.Fatalf("tree rect = %+v, want %+v", got, wantTree)
	}
}

// --- render: model row index -> exact pixel y ----------------------------

// TestVisibleRowsMatchDefaultTree asserts the flattened visible order of
// the default (all-expanded) tree, giving each label a known row index
// and therefore a known y-offset.
func TestVisibleRowsMatchDefaultTree(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	rows := s.visibleRows()
	// The first rows are deterministic: root, then the first package
	// (hugo) and its os/arch -> version subtree. Version leaves carry the
	// publication date after a "   ·   " separator when the row has one;
	// hugo/linux/amd64 0.128.0 has an EMPTY published date in the sample, so
	// that leaf shows the bare version — exercising both label branches.
	want := []visibleRow{
		{"registry", 0},
		{"hugo", 1},
		{"darwin/arm64", 2},
		{"0.129.0" + versionSep + "2026-08-03", 3},
		{"linux/amd64", 2},
		{"0.128.0", 3}, // empty published -> bare version, no separator
		{"jq", 1},
	}
	if len(rows) < len(want) {
		t.Fatalf("only %d visible rows, want at least %d", len(rows), len(want))
	}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("visible row %d = %+v, want %+v", i, rows[i], w)
		}
	}
	// Row y math: hugo is index 1 -> treeTop + 18, jq is index 6.
	if got := s.rowY(1); got != s.tree.Bounds().Y+1*rowHeight {
		t.Fatalf("rowY(1) = %d, want %d", got, s.tree.Bounds().Y+rowHeight)
	}
}

// TestVersionLabel covers both label branches: a dated version and an
// undated one.
func TestVersionLabel(t *testing.T) {
	if got := versionLabel("1.10.0", "2026-08-02"); got != "1.10.0"+versionSep+"2026-08-02" {
		t.Fatalf("dated label = %q", got)
	}
	if got := versionLabel("0.128.0", ""); got != "0.128.0" {
		t.Fatalf("undated label = %q, want bare version", got)
	}
}

// TestVersionLeafCarriesPublishedDate walks the filtered tree and asserts a
// specific version leaf carries its publication date, and that the empty-date
// leaf carries none — so the date is threaded from the model through the
// TreeView labels the WASM view paints.
func TestVersionLeafCarriesPublishedDate(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Find lz4 -> linux/amd64 -> 1.10.0 (published 2026-08-02).
	var lz4 *toolkit.TreeNode
	for _, n := range s.tree.Root.Children {
		if n.Label == "lz4" {
			lz4 = n
		}
	}
	if lz4 == nil {
		t.Fatal("lz4 node missing")
	}
	found := false
	for _, plat := range lz4.Children {
		if plat.Label != "linux/amd64" {
			continue
		}
		for _, leaf := range plat.Children {
			if leaf.Data == "1.10.0" {
				found = true
				if leaf.Label != "1.10.0"+versionSep+"2026-08-02" {
					t.Fatalf("lz4 1.10.0 leaf label = %q, want dated", leaf.Label)
				}
			}
		}
	}
	if !found {
		t.Fatal("lz4 linux/amd64 1.10.0 leaf not found")
	}
	// The undated hugo 0.128.0 leaf must show the bare version (Data holds the
	// version; Label omits any separator).
	for _, n := range s.tree.Root.Children {
		if n.Label != "hugo" {
			continue
		}
		for _, plat := range n.Children {
			for _, leaf := range plat.Children {
				if leaf.Data == "0.128.0" && leaf.Label != "0.128.0" {
					t.Fatalf("undated hugo 0.128.0 leaf label = %q, want bare version", leaf.Label)
				}
			}
		}
	}
}

// TestSelectedRowPaintsAccentAtExpectedY selects the "hugo" package node
// (visible index 1), renders, and asserts the Accent selection band lands
// exactly at treeTop + 1*rowHeight — and that neighbouring rows (the
// root at index 0, "jq" at index 6) do NOT carry the Accent fill. This
// binds the model's row index to a real pixel position.
func TestSelectedRowPaintsAccentAtExpectedY(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	theme := s.theme
	hugo := s.tree.Root.Children[0]
	if hugo.Label != "hugo" {
		t.Fatalf("first package node = %q, want hugo", hugo.Label)
	}
	s.tree.Selected = hugo

	surf := newSurface()
	s.draw(surf)

	treeX := s.tree.Bounds().X
	// hugo row (index 1): its background must be Accent.
	hugoY := s.rowY(1)
	if got := px(surf, treeX+1, hugoY+2); !eqColor(got, theme.Accent) {
		t.Fatalf("selected hugo row at y=%d: pixel %+v, want Accent %+v", hugoY, got, theme.Accent)
	}
	// root row (index 0): NOT selected -> Surface fill, never Accent.
	rootY := s.rowY(0)
	if got := px(surf, treeX+1, rootY+2); eqColor(got, theme.Accent) {
		t.Fatalf("root row at y=%d unexpectedly Accent-filled: %+v", rootY, got)
	}
	if got := px(surf, treeX+1, rootY+2); !eqColor(got, theme.Surface) {
		t.Fatalf("root row at y=%d: pixel %+v, want Surface %+v", rootY, got, theme.Surface)
	}
	// jq row (index 6): also unselected -> Surface, not Accent.
	jqY := s.rowY(6)
	if got := px(surf, treeX+1, jqY+2); eqColor(got, theme.Accent) {
		t.Fatalf("jq row at y=%d unexpectedly Accent-filled: %+v", jqY, got)
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

func TestOSFilterNarrowsTree(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// osDomain = [darwin linux windows]; "windows" is domain index 2 ->
	// switcher segment 3.
	s.osSwitch.OnChange(3)
	if s.osFilter != "windows" {
		t.Fatalf("osFilter = %q, want windows", s.osFilter)
	}
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"ripgrep", "zstd"}) {
		t.Fatalf("os=windows -> %v, want [ripgrep zstd]", got)
	}
	// Back to All.
	s.osSwitch.OnChange(0)
	if s.osFilter != "" {
		t.Fatalf("osFilter after All = %q, want empty", s.osFilter)
	}
	if got := len(s.shownNames()); got != 5 {
		t.Fatalf("os=All count = %d, want 5", got)
	}
}

func TestVersionFilterNarrowsTree(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Find the DropDown option index for version "1.10.0".
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

// TestVersionDropDownPopoverRouting opens the version DropDown via a real
// handleClick, renders the popover, selects an option through a popover
// click, and confirms an outside click dismisses it — exercising the
// host-owned popover draw + click routing.
func TestVersionDropDownPopoverRouting(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	dr := s.verDrop.Bounds()
	// Click the control to open it.
	s.handleClick(dr.X+dr.W/2, dr.Y+dr.H/2)
	if !s.verDrop.Open {
		t.Fatal("clicking the version DropDown should open its popover")
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
		t.Fatal("version DropDown should reopen")
	}
	s.handleClick(1, 1) // top-left, well outside the popover
	if s.verDrop.Open {
		t.Fatal("outside click should dismiss the popover")
	}
}

func TestCombinedFiltersAND(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	s.search.OnChange("lz4")
	// archDomain = [amd64 arm64]; "arm64" -> domain index 1 -> segment 2.
	s.archSwitch.OnChange(2)
	if s.archFilter != "arm64" {
		t.Fatalf("archFilter = %q, want arm64", s.archFilter)
	}
	if got := s.shownNames(); !reflect.DeepEqual(got, []string{"lz4"}) {
		t.Fatalf("lz4 + arm64 -> %v, want [lz4]", got)
	}
	// Under lz4, only arm64 os/arch groups should remain (darwin/arm64,
	// linux/arm64); no amd64 group survives the AND.
	for _, r := range s.visibleRows() {
		if r.Depth == 2 && (r.Label == "linux/amd64") {
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

func TestClickSwitcherSegmentRoutes(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	r := s.osSwitch.Bounds()
	segW := r.W / 4 // All + 3 os values
	// Click segment 3 ("windows").
	s.handleClick(r.X+segW*3+segW/2, r.Y+r.H/2)
	if s.osFilter != "windows" {
		t.Fatalf("clicking os segment 3 set osFilter=%q, want windows", s.osFilter)
	}
	if s.keyTarget != toolkit.Widget(s.osSwitch) || s.search.Focused {
		t.Fatal("clicking a switcher should move focus off the search box")
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

func TestShownNamesAndVisibleRowsNilRoot(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	s.tree.Root = nil
	if s.shownNames() != nil {
		t.Fatal("shownNames with nil root should be nil")
	}
	if s.visibleRows() != nil {
		t.Fatal("visibleRows with nil root should be nil")
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
// so the layout is inspectable, asserting it is a non-trivial image of the
// expected dimensions. When GO_PKGX_DUMP_PNG names a directory the PNG lands
// there too.
func TestRenderDumpsPNG(t *testing.T) {
	s := newState(surfaceW, surfaceH, nil)
	// Select a node so the render also shows the Accent selection band.
	s.tree.Selected = s.tree.Root.Children[0]
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
