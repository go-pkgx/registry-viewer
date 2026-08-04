// Command registry-viewer is a browser wasm app that renders a
// filterable tree view of the go-pkgx package registry onto a plain
// <canvas>, using go-widgets/toolkit. It fetches registry.json from the
// same directory at start-up; if that fetch fails it falls back to the
// registry.json embedded at build time (see scene.go), so the page is
// never blank.
//
// The scene logic lives in scene.go (no build tag) so it is fully
// native-testable; this file is the js/wasm canvas driver only —
// mouse/keyboard dispatch into state.handle*, state.draw into an
// ImageData, and CopyBytesToJS -> putImageData. It carries the
// js && wasm build tag and is excluded from coverage, mirroring the
// go-widgets/gallery template.
//
//go:build js && wasm

package main

import (
	"syscall/js"
)

func main() {
	doc := js.Global().Get("document")
	canvas := doc.Call("getElementById", "screen")
	if canvas.IsUndefined() || canvas.IsNull() {
		println("registry-viewer: no #screen canvas in the host page")
		return
	}
	canvas.Set("width", surfaceW)
	canvas.Set("height", surfaceH)
	ctx := canvas.Call("getContext", "2d")

	local := make([]byte, 4*surfaceW*surfaceH)
	imageData := ctx.Call("createImageData", surfaceW, surfaceH)
	dst := imageData.Get("data")

	// state is built once the registry bytes arrive (fetch or fallback).
	// Every callback guards against a nil state so an early event before
	// the fetch resolves is harmless.
	var state *state

	render := func() {
		if state == nil {
			return
		}
		state.draw(local)
		js.CopyBytesToJS(dst, local)
		ctx.Call("putImageData", imageData, 0, 0)
	}

	// Mouse dispatch: convert clientX/Y to canvas-local pixel coords via
	// the bounding rect, then route press / move / release.
	coords := func(ev js.Value) (int, int) {
		rect := canvas.Call("getBoundingClientRect")
		sx := rect.Get("width").Float() / float64(surfaceW)
		sy := rect.Get("height").Float() / float64(surfaceH)
		x := int((ev.Get("clientX").Float() - rect.Get("left").Float()) / sx)
		y := int((ev.Get("clientY").Float() - rect.Get("top").Float()) / sy)
		return x, y
	}
	listen := func(name string, handle func(x, y int) bool) {
		cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if state == nil || len(args) == 0 {
				return nil
			}
			x, y := coords(args[0])
			if handle(x, y) {
				render()
			}
			return nil
		})
		canvas.Call("addEventListener", name, cb)
	}
	canvas.Call("addEventListener", "mousedown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if state == nil || len(args) == 0 || args[0].Get("button").Int() != 0 {
			return nil
		}
		x, y := coords(args[0])
		if state.handleClick(x, y) {
			render()
		}
		return nil
	}))
	listen("mousemove", func(x, y int) bool { return state.handleMove(x, y) })
	listen("mouseup", func(x, y int) bool { return state.handleRelease(x, y) })

	// Wheel: forward the raw browser scroll to the widget under the pointer
	// as a toolkit EventScroll. Delta is expressed in ROWS (not pixels): the
	// sign of deltaY picks a small fixed row step, and the toolkit's
	// scrollable widget (the TreeTable grid) does all the offset math +
	// clamping itself. preventDefault stops the page from scrolling under us.
	canvas.Call("addEventListener", "wheel", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if state == nil || len(args) == 0 {
			return nil
		}
		ev := args[0]
		ev.Call("preventDefault")
		x, y := coords(ev)
		d := 3
		if ev.Get("deltaY").Float() < 0 {
			d = -3
		}
		if state.handleScroll(x, y, d) {
			render()
		}
		return nil
	}), map[string]any{"passive": false})

	// Keyboard: a single printable rune with no Ctrl/Meta/Alt is text
	// (EventChar) routed to the focused SearchEntry; every named key
	// (Backspace, …) is an EventKeyDown. Listen on the window (a <canvas>
	// is not focusable by default).
	js.Global().Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if state == nil || len(args) == 0 {
			return nil
		}
		ev := args[0]
		key := ev.Get("key").String()
		var changed bool
		if len([]rune(key)) == 1 && !ev.Get("ctrlKey").Bool() && !ev.Get("metaKey").Bool() && !ev.Get("altKey").Bool() {
			changed = state.handleChar(key)
		} else {
			changed = state.handleKeyDown(key)
		}
		if changed {
			ev.Call("preventDefault")
			render()
		}
		return nil
	}))

	// Build the scene from the fetched registry.json, falling back to the
	// embedded sample on any failure, then render.
	build := func(data []byte) {
		state = newState(surfaceW, surfaceH, data)
		render()
	}
	fetchRegistry(build)

	// Park forever so the callbacks live.
	select {}
}

// fetchRegistry fetches registry.json from the page's directory and hands
// its bytes to cb. On any failure (network error, non-OK status) it calls
// cb(nil) so newState falls back to the embedded sample.
func fetchRegistry(cb func([]byte)) {
	g := js.Global()
	fail := js.FuncOf(func(_ js.Value, _ []js.Value) any { cb(nil); return nil })
	then := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			cb(nil)
			return nil
		}
		resp := args[0]
		if !resp.Get("ok").Bool() {
			cb(nil)
			return nil
		}
		got := js.FuncOf(func(_ js.Value, a []js.Value) any {
			if len(a) == 0 {
				cb(nil)
				return nil
			}
			u8 := g.Get("Uint8Array").New(a[0])
			n := u8.Get("length").Int()
			buf := make([]byte, n)
			js.CopyBytesToGo(buf, u8)
			cb(buf)
			return nil
		})
		resp.Call("arrayBuffer").Call("then", got).Call("catch", fail)
		return nil
	})
	g.Call("fetch", "registry.json").Call("then", then).Call("catch", fail)
}
