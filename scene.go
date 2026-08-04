// SPDX-License-Identifier: BSD-3-Clause
//
// Scene state for the go-pkgx registry viewer. Composes a SearchEntry
// (name filter), two ViewSwitcher segmented controls (os / arch), a
// DropDown combobox (version), a TreeView (name -> os/arch -> version)
// and a Statusbar count into a single filterable dashboard, all from
// go-widgets/toolkit.
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

// Surface dimensions. Fixed (not content-derived): the TreeView
// virtualizes + scrolls internally, so the canvas stays a constant
// size. Lives in scene.go (not main.go) so the native scene_test
// compiles without the js && wasm tag.
const (
	surfaceW = 720
	surfaceH = 560
)

// Layout constants. A single outer margin, a uniform inter-row gap,
// and the fixed heights of the search box, the filter switcher strip
// and the bottom status bar. The TreeView flexes to fill the rest.
const (
	margin    = 8
	gap       = 6
	searchH   = 26
	switchH   = 28
	rowHeight = 18 // TreeView row height (also the toolkit default)

	// dropDownRowH is the pixel height of one row in the version DropDown's
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
// the TreeView, the Statusbar and the box layout that positions them.
type state struct {
	w, h  int
	theme *toolkit.Theme

	pkgs []pkg // every registry row, unfiltered

	// Distinct, sorted filter domains. Each ViewSwitcher's Views is
	// {"All"} ++ the domain, so segment 0 is "no filter".
	osDomain   []string
	archDomain []string
	verDomain  []string

	// Active filter state. nameFilter is stored lower-cased for a
	// case-insensitive substring match; an empty os/arch/verFilter
	// means "All" (segment 0).
	nameFilter string
	osFilter   string
	archFilter string
	verFilter  string

	// Widgets. os + arch are low-cardinality, so segmented ViewSwitchers
	// read best; version is high-cardinality (many published versions), so a
	// DropDown combobox is the fitting control — it stays compact and opens a
	// scrollable option popover instead of an unreadable overcrowded strip.
	search     *toolkit.SearchEntry
	osSwitch   *toolkit.ViewSwitcher
	archSwitch *toolkit.ViewSwitcher
	verDrop    *toolkit.DropDown
	tree       *toolkit.TreeView
	status     *toolkit.Statusbar

	// Box layout. root stacks the search box, the filter strip, the
	// tree and the status bar vertically; filterRow packs the three
	// switchers left-to-right.
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

	s.osSwitch = toolkit.NewViewSwitcher(append([]string{"All"}, s.osDomain...), 0)
	s.osSwitch.OnChange = func(i int) { s.osFilter = domainValue(s.osDomain, i); s.rebuild() }
	s.archSwitch = toolkit.NewViewSwitcher(append([]string{"All"}, s.archDomain...), 0)
	s.archSwitch.OnChange = func(i int) { s.archFilter = domainValue(s.archDomain, i); s.rebuild() }
	s.verDrop = toolkit.NewDropDown(append([]string{"All"}, s.verDomain...), 0)
	s.verDrop.OnSelect = func(i int) { s.verFilter = domainValue(s.verDomain, i); s.rebuild() }

	s.tree = toolkit.NewTreeView(nil)
	s.tree.RowHeight = rowHeight

	s.status = toolkit.NewStatusbar([]string{"", "", "go-pkgx / registry-viewer"})

	// --- box layout ------------------------------------------------------
	// root: search (fixed) / filter strip (fixed) / tree (flex) / status
	// (fixed). filterRow: three equal-flex switchers.
	s.filterRow = toolkit.NewHBox()
	s.filterRow.Spacing = gap
	s.filterRow.AddFlex(s.osSwitch, 1)
	s.filterRow.AddFlex(s.archSwitch, 1)
	s.filterRow.AddFlex(s.verDrop, 1)

	s.root = toolkit.NewVBox()
	s.root.Spacing = gap
	s.root.AddFixed(s.search, searchH)
	s.root.AddFixed(s.filterRow, switchH)
	s.root.AddFlex(s.tree, 1)
	s.root.AddFixed(s.status, toolkit.StatusbarH)
	s.root.SetBounds(toolkit.Rect{X: margin, Y: margin, W: w - 2*margin, H: surfaceH - 2*margin})

	// Hit-test order = visual/z order: filters first, then the tree.
	s.clickables = []toolkit.Widget{s.search, s.osSwitch, s.archSwitch, s.verDrop, s.tree}

	s.rebuild()
	return s
}

// versionSep separates a version from its publication date in a version
// leaf label (e.g. "1.10.0   ·   2026-08-02").
const versionSep = "   ·   "

// versionLabel formats a version-leaf label: the bare version when it has
// no known publication date, else "version <sep> date". Keeping the date
// out when empty avoids a dangling separator on rows the factory could not
// date.
func versionLabel(version, published string) string {
	if published == "" {
		return version
	}
	return version + versionSep + published
}

// domainValue maps a ViewSwitcher index to its filter value: index 0
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

// rebuild recomputes the filtered TreeView (name -> os/arch -> version)
// and refreshes the Statusbar count. Called on construction and on every
// filter change. Deterministic: names, os/arch groups and versions are
// each sorted.
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

	root := &toolkit.TreeNode{Label: "registry", Expanded: true}
	for _, name := range names {
		nameNode := &toolkit.TreeNode{Label: name, Expanded: true, Data: name}
		var keys []string
		for k := range byName[name] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			platNode := &toolkit.TreeNode{Label: k, Expanded: true, Data: k}
			var vers []string
			for v := range byName[name][k] {
				vers = append(vers, v)
			}
			sort.Strings(vers)
			for _, v := range vers {
				platNode.Children = append(platNode.Children,
					&toolkit.TreeNode{Label: versionLabel(v, byName[name][k][v]), Data: v})
			}
			nameNode.Children = append(nameNode.Children, platNode)
		}
		root.Children = append(root.Children, nameNode)
	}
	s.tree.Root = root
	s.tree.Selected = nil
	s.tree.ScrollRow = 0

	total := len(distinct(s.pkgs, func(p pkg) string { return p.Name }))
	shown := len(names)
	s.status.SetSegment(0, strconv.Itoa(total)+" packages")
	s.status.SetSegment(1, strconv.Itoa(shown)+" shown")
}

// shownNames returns the package-name nodes currently in the filtered
// tree (the root's children), in visible order. Used by tests to assert
// the filter narrows the set to exactly the expected packages.
func (s *state) shownNames() []string {
	if s.tree.Root == nil {
		return nil
	}
	out := make([]string, 0, len(s.tree.Root.Children))
	for _, c := range s.tree.Root.Children {
		out = append(out, c.Label)
	}
	return out
}

// visibleRow pairs a node's label with its indentation depth, mirroring
// what TreeView flattens + paints.
type visibleRow struct {
	Label string
	Depth int
}

// visibleRows reproduces the TreeView's visible (expand-aware) flattened
// order over the current tree, so a test can predict exactly which label
// paints at which row index — and thus at which y offset (treeTop +
// index*rowHeight). Mirrors TreeView.flatten/walkTree, which are
// unexported.
func (s *state) visibleRows() []visibleRow {
	var out []visibleRow
	var walk func(n *toolkit.TreeNode, depth int)
	walk = func(n *toolkit.TreeNode, depth int) {
		out = append(out, visibleRow{Label: n.Label, Depth: depth})
		if !n.Expanded {
			return
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	if s.tree.Root != nil {
		walk(s.tree.Root, 0)
	}
	return out
}

// rowY returns the surface y-coordinate of the i-th visible tree row
// (with ScrollRow at 0), so tests can assert a label paints at a precise
// position.
func (s *state) rowY(i int) int { return s.tree.Bounds().Y + i*rowHeight }

// draw paints the whole scene onto buf (an RGBA row-major slice). Buf +
// s.w/s.h are wrapped in a PixelPainter so the widget code sees only the
// painter.Painter interface. Background first, then the box layout
// (search / filters / tree / status).
func (s *state) draw(buf []byte) {
	fillBG(buf, s.w, s.h, s.theme.Background)
	p := painter.NewPixelPainter(buf, s.w, s.h)
	s.root.Draw(p, s.theme)
	// The version DropDown's popover floats above the tree (the host owns the
	// popover surface): a ListBox of the options at PopoverBounds.
	if s.verDrop.Open {
		lb := toolkit.NewListBox(s.verDrop.Options)
		lb.Selected = s.verDrop.Selected
		lb.SetBounds(s.verDrop.PopoverBounds())
		lb.Draw(p, s.theme)
	}
}

// handleClick dispatches a click at surface (x, y) to whichever widget it
// falls in, in draw order. Clicking the SearchEntry focuses it for
// keyboard input; clicking anything else (or dead space) clears that
// focus. Mirrors the gallery's clickables dispatch.
func (s *state) handleClick(x, y int) bool {
	ev := toolkit.Event{Kind: toolkit.EventClick, X: x, Y: y}

	// Open version popover first (it floats above everything): a click inside
	// selects that option row; a click outside dismisses it.
	if s.verDrop.Open {
		pb := s.verDrop.PopoverBounds()
		if inside(x, y, pb) {
			s.verDrop.Select((y - pb.Y) / dropDownRowH)
		} else {
			s.verDrop.Open = false
		}
		return true
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
