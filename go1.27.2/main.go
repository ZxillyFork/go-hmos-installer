// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The go1.27.2 command builds and runs an experimental OpenHarmony Go fork.
//
// Install with go install github.com/ZxillyFork/go-hmos-installer/go1.27.2@latest,
// run go1.27.2 download, then use it like the ordinary go command.
// This is a pinned development snapshot, not an official Go release.
package main

import "github.com/ZxillyFork/go-hmos-installer/internal/toolchain"

func main() {
	toolchain.Main(toolchain.Version{
		Name:      "go1.27.2",
		Commit:    "b637b8617624655906b737977f50de5280bf7f65",
		GOVersion: "go1.27.2-hmos-devel",
	})
}
