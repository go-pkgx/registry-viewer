#!/usr/bin/env bash
# Build the registry-viewer wasm bundle into dist/.
#
# Usage:
#   ./build.sh [registry.json]
#
# Produces dist/{app.wasm, wasm_exec.js, index.html, registry.json}. If a
# registry.json path is passed as $1 it is copied into dist/ (this is what the
# go-pkgx/packages pages.yml passes — the freshly generated registry); with no
# argument the committed sample registry.json is used, so the page still works
# standalone.
#
# CGO is disabled (pure Go). The scene logic is native-testable; only main.go
# carries the js && wasm tag.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
dist="$here/dist"
mkdir -p "$dist"

echo "==> building app.wasm"
( cd "$here" && GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -o "$dist/app.wasm" . )

echo "==> copying wasm_exec.js"
goroot="$(go env GOROOT)"
if [ -f "$goroot/lib/wasm/wasm_exec.js" ]; then
  cp "$goroot/lib/wasm/wasm_exec.js" "$dist/wasm_exec.js"
elif [ -f "$goroot/misc/wasm/wasm_exec.js" ]; then
  cp "$goroot/misc/wasm/wasm_exec.js" "$dist/wasm_exec.js"
else
  echo "error: wasm_exec.js not found under $goroot (lib/wasm or misc/wasm)" >&2
  exit 1
fi

echo "==> copying index.html"
cp "$here/index.html" "$dist/index.html"

echo "==> copying registry.json"
if [ "${1:-}" != "" ]; then
  cp "$1" "$dist/registry.json"
  echo "    (from $1)"
else
  cp "$here/registry.json" "$dist/registry.json"
  echo "    (committed sample)"
fi

echo "==> done: $dist"
ls -la "$dist"
