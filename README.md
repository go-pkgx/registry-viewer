# registry-viewer

A pure-Go, `CGO_ENABLED=0` **WASM** browser app that renders a filterable tree
view of the [`ghcr.io/go-pkgx/packages`](https://github.com/go-pkgx/packages)
registry — every package across linux, darwin and windows — using the
[go-widgets/toolkit](https://github.com/go-widgets/toolkit) `TreeView`, drawn to
a `<canvas>` via [go-widgets/painter](https://github.com/go-widgets/painter).

It powers the landing tree at <https://go-pkgx.github.io/packages/>. Data comes
from a `registry.json` emitted by the packages factory's `pages.yml`; filter by
name, OS, arch and version.

BSD-3-Clause © the go-pkgx/registry-viewer authors.
