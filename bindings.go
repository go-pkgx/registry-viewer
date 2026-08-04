// SPDX-License-Identifier: BSD-3-Clause
//
// One-way view-model -> widget bindings that address a widget's value field
// directly (a raw &field pointer). The scene never assigns a widget field; the
// mvvmtk helpers cover the two-way filter widgets and the forest, and these
// generic mvvm.OneWay sinks cover the rest. They are isolated here in a
// *_binding.go file — the mvvmlint escape hatch — so the scene logic stays free
// of any direct widget-state mutation.

package main

import (
	"github.com/go-widgets/mvvm"
)

// bindWidgets wires every remaining derived Observable to its widget field
// through mvvm.OneWay (view-model -> widget only; the widgets never write back
// through these seams). Each seeds its field from the Observable now and repaints
// on every later change:
//
//   - focused   -> SearchEntry.Focused   (caret visibility)
//   - selection -> TreeTable.Selected     (reset on every rebuild)
//   - scroll    -> TreeTable.ScrollRow     (reset on every rebuild)
//   - totalText -> Statusbar.Segments[0]   ("N packages")
//   - shownText -> Statusbar.Segments[1]   ("M shown")
//
// The Statusbar slice has three seeded segments, so &Segments[0] / &Segments[1]
// are stable element pointers (the app never resizes it). No invalidate hook is
// needed: main.go re-renders after each handled event, synchronously after the
// binding has propagated.
func bindWidgets(s *state) {
	mvvm.OneWay(s.focused, &s.search.Focused, nil)
	mvvm.OneWay(s.selection, &s.grid.Selected, nil)
	mvvm.OneWay(s.scroll, &s.grid.ScrollRow, nil)
	mvvm.OneWay(s.totalText, &s.status.Segments[0], nil)
	mvvm.OneWay(s.shownText, &s.status.Segments[1], nil)
}
