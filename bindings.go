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
// SearchEntry caret visibility is NOT sunk here: the toolkit's focus-owning root
// (the scene's VBox) owns keyboard focus once a click routes through it, so the
// SearchEntry lights its own caret from its embedded focus state. The scene only
// reads that focus (state.hasFocus) — it never writes it — so there is no focus
// Observable to bind.
func bindWidgets(s *state) {
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
