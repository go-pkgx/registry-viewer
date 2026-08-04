// SPDX-License-Identifier: BSD-3-Clause
//
// Scene state for the go-pkgx registry viewer. Composes a SearchEntry
// (name filter), three DropDown combos (os / arch / version filters), a
// TreeTable columned tree grid (Package: name -> os/arch -> version;
// Published: the version's date) and a Statusbar count into a single
// filterable dashboard, all from go-widgets/toolkit.
//
// Kept in a separate file with NO js/wasm build tag (main.go carries
// it) so a native `go test` can exercise the whole scene — parse,
// filter, layout, draw — against a plain RGBA byte buffer via
// painter.NewPixelPainter. See scene_test.go.

package main

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// embeddedRegistry is the committed sample registry, embedded so the
// wasm page is never blank even when the runtime fetch of registry.json
// fails (offline, 404, CORS). newState falls back to it whenever the
// caller-supplied bytes are absent or unparseable.
//
//go:embed registry.json
var embeddedRegistry []byte

// Surface dimensions. Fixed (not content-derived): the TreeTable
// virtualizes + scrolls internally, so the canvas stays a constant
// size. Lives in scene.go (not main.go) so the native scene_test
// compiles without the js && wasm tag.
const (
	surfaceW = 720
	surfaceH = 560
)

// Layout constants. A single outer margin, a uniform inter-row gap,
// and the fixed heights of the search box, the filter (combo) strip and
// the bottom status bar. The TreeTable grid flexes to fill the rest.
const (
	margin  = 8
	gap     = 6
	searchH = 26
	filterH = 28

	// gridPubColW is the fixed pixel width of the Published (date) column;
	// the Package tree column takes the remaining (auto) width.
	gridPubColW = 132

	// dropDownRowH is the pixel height of one row in a filter DropDown's
	// popover (matches the toolkit's PopoverBounds row step), used to map a
	// click inside the popover back to an option index.
	dropDownRowH = 18
)

// pkg is one published package/platform/version row from registry.json.
// Published is the ISO date (YYYY-MM-DD) the version/platform was
// published to the registry; it may be empty ("") when unknown (the
// packages factory fills it from the ghcr version's created_at). It is
// tolerated missing — a row with no "published" key unmarshals to "".
type pkg struct {
	Name      string `json:"name"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
	Published string `json:"published"`
}

// state is the whole scene: the parsed registry, the filter widgets,
// the TreeTable grid, the Statusbar and the box layout that positions them.
type state struct {
	w, h  int
	theme *toolkit.Theme

	pkgs []pkg // every registry row, unfiltered

	// Distinct, sorted filter domains. Each filter DropDown's Options is
	// {"All"} ++ the domain, so option 0 is "no filter".
	osDomain   []string
	archDomain []string
	verDomain  []string

	// Active filter state. nameFilter is stored lower-cased for a
	// case-insensitive substring match; an empty os/arch/verFilter
	// means "All" (option 0).
	nameFilter string
	osFilter   string
	archFilter string
	verFilter  string

	// Widgets. A SearchEntry filters by name; three DropDown combos filter
	// os / arch / version. The registry itself is a TreeTable (columned tree
	// grid): a Package tree column carrying name -> os/arch -> version, and a
	// Published date column on the version-leaf rows.
	search   *toolkit.SearchEntry
	osDrop   *toolkit.DropDown
	archDrop *toolkit.DropDown
	verDrop  *toolkit.DropDown
	grid     *toolkit.TreeTable
	status   *toolkit.Statusbar

	// Box layout. root stacks the search box, the filter (combo) strip, the
	// grid and the status bar vertically; filterRow packs the three combos
	// left-to-right.
	root      *toolkit.VBox
	filterRow *toolkit.HBox

	// Live hit-test list (draw order) + the keyboard-focused widget
	// (the SearchEntry once clicked), mirroring the gallery template.
	clickables []toolkit.Widget
	keyTarget  toolkit.Widget
}

// parseRegistry unmarshals the registry.json byte form into rows. A nil
// or malformed input yields (nil, err); callers fall back to the
// embedded sample.
func parseRegistry(data []byte) ([]pkg, error) {
	var rows []pkg
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// distinct returns the sorted unique values produced by sel over rows.
func distinct(rows []pkg, sel func(pkg) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		v := sel(r)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// newState parses data (falling back to the embedded sample on nil /
// parse error), builds every widget, lays them out and computes the
// first filtered tree. The second parameter is currently ignored (the
// surface height is fixed); it is accepted so the signature reads like
// the gallery template's newState(w, h).
func newState(w, _ int, data []byte) *state {
	// Anti-aliased, shaped go-opentype text (matching the gallery), so
	// the tree + labels render crisply. Done first, before any layout
	// measures text. On the impossible parse error the toolkit keeps its
	// bitmap default, so the scene still renders.
	_ = toolkit.UseOpenTypeText()

	rows, err := parseRegistry(data)
	if err != nil || len(rows) == 0 {
		rows, _ = parseRegistry(embeddedRegistry)
	}

	s := &state{
		w:     w,
		h:     surfaceH,
		theme: toolkit.DefaultLight(),
		pkgs:  rows,
	}
	s.osDomain = distinct(rows, func(p pkg) string { return p.OS })
	s.archDomain = distinct(rows, func(p pkg) string { return p.Arch })
	s.verDomain = distinct(rows, func(p pkg) string { return p.Version })

	// --- widgets ---------------------------------------------------------
	s.search = toolkit.NewSearchEntry("")
	s.search.OnChange = func(text string) {
		s.nameFilter = strings.ToLower(strings.TrimSpace(text))
		s.rebuild()
	}

	s.osDrop = toolkit.NewDropDown(append([]string{"All"}, s.osDomain...), 0)
	s.osDrop.OnSelect = func(i int) { s.osFilter = domainValue(s.osDomain, i); s.rebuild() }
	s.archDrop = toolkit.NewDropDown(append([]string{"All"}, s.archDomain...), 0)
	s.archDrop.OnSelect = func(i int) { s.archFilter = domainValue(s.archDomain, i); s.rebuild() }
	s.verDrop = toolkit.NewDropDown(append([]string{"All"}, s.verDomain...), 0)
	s.verDrop.OnSelect = func(i int) { s.verFilter = domainValue(s.verDomain, i); s.rebuild() }

	// TreeTable (columned tree grid): Package (tree column, auto width) +
	// Published (fixed date column, right-aligned so dates line up).
	s.grid = toolkit.NewTreeTable([]toolkit.TreeTableColumn{
		{Title: "Package"},
		{Title: "Published", Width: gridPubColW, Align: toolkit.AlignRight},
	}, nil)

	s.status = toolkit.NewStatusbar([]string{"", "", "go-pkgx / registry-viewer"})

	// --- box layout ------------------------------------------------------
	// root: search (fixed) / filter strip (fixed) / grid (flex) / status
	// (fixed). filterRow: three equal-flex combo boxes.
	s.filterRow = toolkit.NewHBox()
	s.filterRow.Spacing = gap
	s.filterRow.AddFlex(s.osDrop, 1)
	s.filterRow.AddFlex(s.archDrop, 1)
	s.filterRow.AddFlex(s.verDrop, 1)

	s.root = toolkit.NewVBox()
	s.root.Spacing = gap
	s.root.AddFixed(s.search, searchH)
	s.root.AddFixed(s.filterRow, filterH)
	s.root.AddFlex(s.grid, 1)
	s.root.AddFixed(s.status, toolkit.StatusbarH)
	s.root.SetBounds(toolkit.Rect{X: margin, Y: margin, W: w - 2*margin, H: surfaceH - 2*margin})

	// Hit-test order = visual/z order: filters first, then the grid.
	s.clickables = []toolkit.Widget{s.search, s.osDrop, s.archDrop, s.verDrop, s.grid}

	s.rebuild()
	return s
}

// dropdowns returns the three filter combos in a fixed order, so the
// popover draw + click routing can iterate them uniformly.
func (s *state) dropdowns() []*toolkit.DropDown {
	return []*toolkit.DropDown{s.osDrop, s.archDrop, s.verDrop}
}

// domainValue maps a DropDown option index to its filter value: index 0
// ("All") is the empty "no filter" string; index i>0 is domain[i-1].
func domainValue(domain []string, i int) string {
	if i <= 0 || i > len(domain) {
		return ""
	}
	return domain[i-1]
}

// passes reports whether p survives every active filter (combined with
// AND). An empty os/arch/verFilter or nameFilter is "no constraint".
func (s *state) passes(p pkg) bool {
	if s.nameFilter != "" && !strings.Contains(strings.ToLower(p.Name), s.nameFilter) {
		return false
	}
	if s.osFilter != "" && p.OS != s.osFilter {
		return false
	}
	if s.archFilter != "" && p.Arch != s.archFilter {
		return false
	}
	if s.verFilter != "" && p.Version != s.verFilter {
		return false
	}
	return true
}

// rebuild recomputes the filtered TreeTable forest (name -> os/arch ->
// version, with the publish date in the version leaf's Published cell) and
// refreshes the Statusbar count. Called on construction and on every filter
// change. Deterministic: names, os/arch groups and versions are each sorted.
func (s *state) rebuild() {
	// name -> "os/arch" -> version -> published date. The registry has one
	// row per (name, os, arch, version), so a plain assignment (last wins)
	// carries each version's publication date without a merge branch.
	byName := map[string]map[string]map[string]string{}
	var names []string
	for _, p := range s.pkgs {
		if !s.passes(p) {
			continue
		}
		if byName[p.Name] == nil {
			byName[p.Name] = map[string]map[string]string{}
			names = append(names, p.Name)
		}
		key := p.OS + "/" + p.Arch
		if byName[p.Name][key] == nil {
			byName[p.Name][key] = map[string]string{}
		}
		byName[p.Name][key][p.Version] = p.Published
	}
	sort.Strings(names)

	// Forest of package-name nodes (TreeTable takes a forest, so there is no
	// synthetic root). Cells[0] is the Package tree column; Cells[1] is the
	// Published date column — populated only on the version leaves (name +
	// os/arch rows leave it blank).
	var forest []*toolkit.TreeTableNode
	for _, name := range names {
		nameNode := &toolkit.TreeTableNode{Cells: []string{name}, Expanded: true}
		var keys []string
		for k := range byName[name] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			platNode := &toolkit.TreeTableNode{Cells: []string{k}, Expanded: true}
			var vers []string
			for v := range byName[name][k] {
				vers = append(vers, v)
			}
			sort.Strings(vers)
			for _, v := range vers {
				platNode.Children = append(platNode.Children,
					&toolkit.TreeTableNode{Cells: []string{v, byName[name][k][v]}})
			}
			nameNode.Children = append(nameNode.Children, platNode)
		}
		forest = append(forest, nameNode)
	}
	s.grid.Root = forest
	s.grid.Selected = nil
	s.grid.ScrollRow = 0

	total := len(distinct(s.pkgs, func(p pkg) string { return p.Name }))
	shown := len(names)
	s.status.SetSegment(0, strconv.Itoa(total)+" packages")
	s.status.SetSegment(1, strconv.Itoa(shown)+" shown")
}

// shownNames returns the top-level package-name rows currently in the
// filtered grid (the forest roots), in visible order. Used by tests to
// assert the filter narrows the set to exactly the expected packages.
func (s *state) shownNames() []string {
	out := make([]string, 0, len(s.grid.Root))
	for _, n := range s.grid.Root {
		out = append(out, cellAt(n, 0))
	}
	return out
}

// cellAt returns node's j-th cell text, or "" when the node carries fewer
// cells (the same short-row tolerance TreeTable itself applies).
func cellAt(n *toolkit.TreeTableNode, j int) string {
	if j < len(n.Cells) {
		return n.Cells[j]
	}
	return ""
}

// visibleRow captures one flattened grid row: its Package (tree) cell, its
// Published cell and its indentation depth — everything a test needs to
// predict which row paints where.
type visibleRow struct {
	Label     string
	Published string
	Depth     int
}

// visibleRows reproduces the TreeTable's visible (expand-aware) flattened
// forest order, so a test can predict exactly which row paints at which
// body index — and thus at which y offset (bodyTop + index*rowHeight).
// Mirrors TreeTable.flatten/walk, which are unexported.
func (s *state) visibleRows() []visibleRow {
	var out []visibleRow
	var walk func(n *toolkit.TreeTableNode, depth int)
	walk = func(n *toolkit.TreeTableNode, depth int) {
		out = append(out, visibleRow{Label: cellAt(n, 0), Published: cellAt(n, 1), Depth: depth})
		if !n.Expanded {
			return
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for _, n := range s.grid.Root {
		walk(n, 0)
	}
	return out
}

// rowY returns the surface y-coordinate of the i-th visible BODY row (with
// ScrollRow at 0): the grid's top, past the header, plus i row heights. So
// tests can assert a row paints at a precise position.
func (s *state) rowY(i int) int {
	return s.grid.Bounds().Y + toolkit.TreeTableHeaderHeight + i*toolkit.TreeTableRowHeight
}

// draw paints the whole scene onto buf (an RGBA row-major slice). Buf +
// s.w/s.h are wrapped in a PixelPainter so the widget code sees only the
// painter.Painter interface. Background first, then the box layout
// (search / filter combos / grid / status), then any open combo popover.
func (s *state) draw(buf []byte) {
	fillBG(buf, s.w, s.h, s.theme.Background)
	p := painter.NewPixelPainter(buf, s.w, s.h)
	s.root.Draw(p, s.theme)
	// A filter DropDown's popover floats above the grid (the host owns the
	// popover surface): a ListBox of the options at PopoverBounds.
	for _, d := range s.dropdowns() {
		if d.Open {
			lb := toolkit.NewListBox(d.Options)
			lb.Selected = d.Selected
			lb.SetBounds(d.PopoverBounds())
			lb.Draw(p, s.theme)
		}
	}
}

// handleClick dispatches a click at surface (x, y) to whichever widget it
// falls in, in draw order. Clicking the SearchEntry focuses it for
// keyboard input; clicking anything else (or dead space) clears that
// focus. Mirrors the gallery's clickables dispatch.
func (s *state) handleClick(x, y int) bool {
	ev := toolkit.Event{Kind: toolkit.EventClick, X: x, Y: y}

	// An open combo popover floats above everything: a click inside selects
	// that option row; a click outside dismisses it.
	for _, d := range s.dropdowns() {
		if d.Open {
			pb := d.PopoverBounds()
			if inside(x, y, pb) {
				d.Select((y - pb.Y) / dropDownRowH)
			} else {
				d.Open = false
			}
			return true
		}
	}

	for _, w := range s.clickables {
		r := w.Bounds()
		if inside(x, y, r) {
			s.keyTarget = w
			s.search.Focused = (w == toolkit.Widget(s.search))
			w.OnEvent(local(ev, r))
			return true
		}
	}
	s.keyTarget = nil
	s.search.Focused = false
	return true
}

// handleScroll routes a wheel scroll at surface (x, y) — Delta in ROWS,
// positive = down — to whichever widget it falls in, via the root VBox's
// coordinate routing (exactly like a click). The scrollable widget (the
// TreeTable grid) consumes the EventScroll and scrolls itself, clamping at
// both ends: the app does NO scroll math. Always reports true so the driver
// re-renders (a scroll over dead space is a harmless no-op paint).
func (s *state) handleScroll(x, y, delta int) bool {
	ev := toolkit.Event{Kind: toolkit.EventScroll, X: x, Y: y, Delta: delta}
	s.root.OnEvent(local(ev, s.root.Bounds()))
	return true
}

// handleMove is a no-op hover hook (the scene has no hover affordances);
// kept so main.go can wire mousemove uniformly with the gallery driver.
func (s *state) handleMove(x, y int) bool { _, _ = x, y; return false }

// handleRelease is a no-op (no drag targets in this scene); kept for a
// uniform main.go event-wiring surface.
func (s *state) handleRelease(x, y int) bool { _, _ = x, y; return false }

// handleChar routes a printable character to the focused widget (the
// SearchEntry) as an EventChar, so the name filter updates live as the
// user types. Reports whether a target consumed it.
func (s *state) handleChar(ch string) bool {
	if s.keyTarget == nil {
		return false
	}
	s.keyTarget.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: ch})
	return true
}

// handleKeyDown routes a named key (Backspace, …) to the focused widget
// as an EventKeyDown. Reports whether a target consumed it.
func (s *state) handleKeyDown(code string) bool {
	if s.keyTarget == nil {
		return false
	}
	s.keyTarget.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: code})
	return true
}

// --- helpers --------------------------------------------------------------

// fillBG paints the entire RGBA buffer with c (opaque background).
func fillBG(buf []byte, w, h int, c toolkit.RGBA) {
	for i := 0; i+3 < len(buf); i += 4 {
		buf[i], buf[i+1], buf[i+2], buf[i+3] = c.R, c.G, c.B, c.A
	}
	_, _ = w, h
}

// inside reports whether (x, y) falls in r (half-open on the far edges).
func inside(x, y int, r toolkit.Rect) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// local re-bases a surface-space event into r's widget-local coordinates.
func local(ev toolkit.Event, r toolkit.Rect) toolkit.Event {
	ev.X -= r.X
	ev.Y -= r.Y
	return ev
}
