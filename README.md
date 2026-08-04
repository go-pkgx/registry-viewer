# registry-viewer

A pure-Go, `CGO_ENABLED=0` **WASM** browser app that renders a filterable
tree-grid of the [`ghcr.io/go-pkgx/packages`](https://github.com/go-pkgx/packages)
registry — every package across linux, darwin and windows — using the
[go-widgets/toolkit](https://github.com/go-widgets/toolkit) widget set, drawn to
a `<canvas>` via [go-widgets/painter](https://github.com/go-widgets/painter).

It powers the landing tree at <https://go-pkgx.github.io/packages/>. Data comes
from a `registry.json` emitted by the packages factory's `pages.yml`.

## What it does

The registry is shown as a **`TreeTable`** (columned tree grid) with two columns:

- **Package** — the tree column carrying the **name → os/arch → version**
  hierarchy;
- **Published** — the version's publication date (ISO `YYYY-MM-DD`), in its own
  right-aligned column on the version-leaf rows (blank when unknown).

Filters:

- a **`SearchEntry`** filters by package **name** (case-insensitive substring,
  live as you type);
- three **`DropDown`** combos filter by **os**, **arch** and **version**;
- filters combine with **AND**, and the **`Statusbar`** shows a live count
  (`N packages`, `M shown`).

Layout is composed from toolkit `VBox`/`HBox` boxes — no hand-rolled drawing.
When WebAssembly is unavailable, `index.html` falls back to a plain-HTML
tree-grid table (same columns, `<select>` combos) behind a loading progress bar.

At runtime the app `fetch()`es `registry.json` from its own directory; if that
fails it falls back to a `registry.json` embedded at build time (`go:embed`), so
the page is never blank.

## Build

```sh
export GOWORK=off GOPROXY=direct
./build.sh                 # uses the committed sample registry.json
./build.sh /path/to/registry.json   # or a generated one (what pages.yml passes)
```

`build.sh` compiles `app.wasm` (`GOOS=js GOARCH=wasm CGO_ENABLED=0`), copies
Go's `wasm_exec.js`, `index.html` and the chosen `registry.json` into `dist/`.
Serve `dist/` over HTTP and open it.

## Test

The scene logic (`scene.go`) carries no build tag, so it is fully native-testable
— only the wasm canvas driver (`main.go`) is `//go:build js && wasm`. The tests
render the scene to an `image.RGBA` through `painter.NewPixelPainter` and assert
position-precise bounds (the search box / filter strip / grid rectangles, the
column header band + separator, the selected-row Accent band at its exact row
y-offset, and a dated Published cell painting ink in its column at a known row),
plus that each filter narrows the visible package set to the expected packages.

```sh
GOWORK=off go test ./...          # 100% coverage of the scene/filter logic
```

A render is dumped to `testdata/render.png` for visual inspection.

BSD-3-Clause © the go-pkgx/registry-viewer authors.
