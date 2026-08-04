// SPDX-License-Identifier: BSD-3-Clause
//
// Native entrypoint. The real app is the wasm canvas driver in main.go
// (build-tagged js && wasm); this stub exists only so `go build ./...`
// succeeds on a native host — a `package main` with no `main` function
// otherwise fails to link. Running the native binary just explains that
// this is a browser wasm app and points at build.sh. The scene logic in
// scene.go is what the native test suite exercises.
//
//go:build !(js && wasm)

package main

import "fmt"

// banner is the message the native binary prints. Kept as a pure helper
// so it is unit-testable without capturing stdout.
func banner() string {
	return "registry-viewer is a browser wasm app.\n" +
		"Build the web bundle with ./build.sh, then serve dist/ over HTTP."
}

func main() { fmt.Println(banner()) }
