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
//   - focused   -> SearchEntry.SetFocused (caret visibility)
//   - selection -> TreeTable.Selected     (reset on every rebuild)
//   - scroll    -> TreeTable.ScrollRow     (reset on every rebuild)
//   - totalText -> Statusbar.Segments[0]   ("N packages")
//   - shownText -> Statusbar.Segments[1]   ("M shown")
//
// The Statusbar slice has three seeded segments, so &Segments[0] / &Segments[1]
// are stable element pointers (the app never resizes it). No invalidate hook is
// needed: main.go re-renders after each handled event, synchronously after the
// binding has propagated.
//
// The focus sink is the one that cannot use a raw &field pointer: toolkit
// v0.108.0 replaced SearchEntry's exported Focused bool with the Focusable
// SetFocused/Focused method pair, so caret visibility is driven through the
// setter by oneWaySet rather than mvvm.OneWay. A method call is not a selector
// assignment, so mvvmlint leaves it alone.
func bindWidgets(s *state) {
	oneWaySet(s.focused, s.search.SetFocused)
	oneWaySet(s.selection, s.grid.Selected().Set)
	oneWaySet(s.scroll, s.grid.ScrollRow().Set)
	mvvm.OneWay(s.totalText, &s.status.Segments[0], nil)
	mvvm.OneWay(s.shownText, &s.status.Segments[1], nil)
}

// oneWaySet is the setter-driven counterpart of mvvm.OneWay for a widget that
// exposes its state through a method (e.g. a Focusable's SetFocused) rather than
// an addressable field. It mirrors OneWay's contract: seed the widget from the
// Observable now, then push every later value through set. The returned unbind
// detaches the subscription.
func oneWaySet[T any](obs *mvvm.Observable[T], set func(T)) (unbind func()) {
	set(obs.Get())
	return obs.Subscribe(set)
}
