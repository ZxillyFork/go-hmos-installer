// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The go1.27.4-hmos command builds an experimental OpenHarmony Go fork.
// Install using github.com/ZxillyFork/go-hmos-installer/go1.27.4-hmos.
package main

import "github.com/ZxillyFork/go-hmos-installer/internal/toolchain"

func main() {
	toolchain.Main(toolchain.Version{
		Name:      "go1.27.4-hmos",
		Commit:    "81bad8b625b9c845961010ea2af776a3cb426b4a",
		GOVersion: "go1.27.4-hmos-devel",
	})
}
