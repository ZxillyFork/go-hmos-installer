// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package toolchain

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testCommit = "0123456789012345678901234567890123456789"

func TestBootstrapVersion(t *testing.T) {
	for version, want := range map[string]bool{"go1.24.5": false, "go1.24.6": true, "go1.24.6garbage": false, "go1.24rc2": false, "go1.25.0": true, "go1.27.1": true, "go1.27.1-hmos-devel": true, "go1.27.2": true, "go1.27.2-hmos-devel": true, "go1.9.9": false, "devel go1.28": false, "garbage": false} {
		if got := bootstrapVersionOK(version); got != want {
			t.Errorf("%q: got %v, want %v", version, got, want)
		}
	}
}

func TestEnvironment(t *testing.T) {
	env := []string{"PATH=/bin", "GOOS=openharmony", "GOARCH=arm64", "GOROOT=/wrong", "GOTOOLCHAIN=auto", "GOFLAGS=-race", "GOEXPERIMENT=bad", "CGO_ENABLED=1", "CC=bad-clang", "CC_FOR_TARGET=bad", "CXX=bad", "GOPATH=/wrong", "HOME=/home/a b"}
	clean := cleanBuildEnv(env, "/bootstrap go", "/cache go")
	values := map[string]string{}
	for _, item := range clean {
		k, v, _ := strings.Cut(item, "=")
		values[k] = v
	}
	for key, want := range map[string]string{"PATH": "/bin", "HOME": "/home/a b", "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH, "GOTOOLCHAIN": "local", "CGO_ENABLED": "0", "GOROOT_BOOTSTRAP": "/bootstrap go", "GOCACHE": "/cache go", "GOENV": "off"} {
		if values[key] != want {
			t.Errorf("%s=%q, want %q", key, values[key], want)
		}
	}
	for _, key := range []string{"GOROOT", "CC", "CXX", "CC_FOR_TARGET", "GOEXPERIMENT", "GOPATH"} {
		if _, ok := values[key]; ok {
			t.Errorf("leaked %s", key)
		}
	}
	forwarded := replaceEnv(env, map[string]string{"GOROOT": "/new go", "GOTOOLCHAIN": "local"})
	if !strings.Contains(strings.Join(forwarded, "\n"), "GOOS=openharmony\n") {
		t.Fatal("forwarding lost target")
	}
	if strings.Contains(strings.Join(forwarded, "\n"), "GOTOOLCHAIN=auto") {
		t.Fatal("automatic stock-Go switching enabled")
	}
}

func cachedToolchain(t *testing.T, home, commit string) string {
	t.Helper()
	root := rootPath(home, commit)
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", executable("go")), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gohmos-complete"), []byte(commit+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCachedInstallAndRecovery(t *testing.T) {
	home := filepath.Join(t.TempDir(), "SDK with spaces")
	want := cachedToolchain(t, home, testCommit)
	i := installer{home: home, commit: testCommit, repository: "must not be contacted", out: io.Discard}
	for n := 0; n < 2; n++ {
		got, err := i.install()
		if err != nil || got != want {
			t.Fatalf("install: %q %v", got, err)
		}
		selected, err := selectedRoot(home)
		if err != nil || selected != want {
			t.Fatalf("selection: %q %v", selected, err)
		}
	}
	// A failed upgrade must not replace the current selection or existing tree.
	i.commit = "1111111111111111111111111111111111111111"
	i.bootstrap = filepath.Join(home, "missing bootstrap")
	if _, err := i.install(); err == nil {
		t.Fatal("expected failure")
	}
	got, err := selectedRoot(home)
	if err != nil || got != want {
		t.Fatalf("old selection lost: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".install-lock")); !os.IsNotExist(err) {
		t.Fatalf("lock left behind: %v", err)
	}
}

func TestLockAndIncompleteDirectory(t *testing.T) {
	home := t.TempDir()
	i := installer{home: home, commit: testCommit, out: io.Discard}
	lock := filepath.Join(home, ".install-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := i.install(); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("want lock error, got %v", err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootPath(home, testCommit), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := i.install(); err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("want incomplete error, got %v", err)
	}
}

func TestMetadataValidation(t *testing.T) {
	home := t.TempDir()
	for _, current := range []installation{{Commit: "../../escape", Host: host()}, {Commit: testCommit, Host: "wrong-host"}, {Commit: testCommit, Host: host()}} {
		b, _ := json.Marshal(current)
		if err := os.WriteFile(filepath.Join(home, "current.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := selectedRoot(home); err == nil {
			t.Fatal("accepted invalid or incomplete toolchain")
		}
	}
}

func TestAtomicWrite(t *testing.T) {
	name := filepath.Join(t.TempDir(), "space name")
	for _, value := range []string{"first", "second"} {
		if err := atomicWrite(name, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(name)
		if err != nil || string(got) != value {
			t.Fatalf("%q %v", got, err)
		}
	}
}

// Explicit opt-in: runs the actual source bootstrap from a local checkout,
// including paths with spaces and a repeated cached install. No remote needed.
func TestRealInstallation(t *testing.T) {
	repository := os.Getenv("GOHMOS_TEST_REPOSITORY")
	commit := os.Getenv("GOHMOS_TEST_COMMIT")
	if repository == "" {
		t.Skip("set GOHMOS_TEST_REPOSITORY to run source bootstrap (GOHMOS_TEST_COMMIT defaults to sourceCommit)")
	}
	if commit == "" {
		commit = sourceCommit
	}
	home := filepath.Join(t.TempDir(), "SDK with spaces")
	t.Setenv("GIT_DIR", filepath.Join(home, "unrelated repo"))
	t.Setenv("GIT_WORK_TREE", filepath.Join(home, "unrelated worktree"))
	i := installer{home: home, repository: repository, commit: commit, bootstrap: runtime.GOROOT(), out: os.Stderr}
	root, err := i.install()
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(filepath.Join(root, "bin", executable("go")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i.install(); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(filepath.Join(root, "bin", executable("go")))
	if err != nil || !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("cached install rebuilt toolchain")
	}
	selected, err := selectedRoot(home)
	if err != nil || selected != root {
		t.Fatalf("selection %q %v", selected, err)
	}
	goexe := filepath.Join(root, "bin", executable("go"))
	cmd := exec.Command(goexe, "env", "GOROOT")
	cmd.Env = replaceEnv(os.Environ(), map[string]string{"GOROOT": root, "GOENV": "off", "GOTOOLCHAIN": "local", "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH})
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != root {
		t.Fatalf("relocated GOROOT: %s %v", output, err)
	}
	program := filepath.Join(home, "hello.go")
	if err := os.WriteFile(program, []byte("package main\nfunc main() { println(\"gohmos host smoke passed\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(goexe, "run", program)
	cmd.Env = cleanBuildEnv(os.Environ(), runtime.GOROOT(), filepath.Join(home, "cache"))
	cmd.Env = replaceEnv(cmd.Env, map[string]string{"GOROOT": root})
	output, err = cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("gohmos host smoke passed")) {
		t.Fatalf("host smoke: %s %v", output, err)
	}
	if commit == sourceCommit {
		cmd = exec.Command(goexe, "env", "GOVERSION")
		cmd.Env = replaceEnv(cleanBuildEnv(os.Environ(), runtime.GOROOT(), filepath.Join(home, "cache")), map[string]string{"GOROOT": root})
		output, err = cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "go1.27.1-hmos-devel" {
			t.Fatalf("legacy toolchain version: %s %v", output, err)
		}
	}
	// Exercise the public install path when the local source uses the legacy pin.
	// This verifies launcher copying, custom-home discovery, forwarding, and a
	// cached install through the installed launcher itself, without network.
	if commit == sourceCommit {
		temporaryLauncher := filepath.Join(home, executable("temporary-launcher"))
		cmd = exec.Command(filepath.Join(runtime.GOROOT(), "bin", executable("go")), "build", "-buildvcs=false", "-o", temporaryLauncher, "../..")
		cmd.Env = isolatedGitEnv(os.Environ())
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build launcher: %s %v", output, err)
		}
		cmd = exec.Command(temporaryLauncher, "install", "--home", home)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("public install: %s %v", output, err)
		}
		launcher := filepath.Join(home, "bin", executable("gohmos"))
		for _, args := range [][]string{{"install"}, {"--version"}, {"version"}, {"env", "GOOS"}} {
			cmd = exec.Command(launcher, args...)
			cmd.Env = replaceEnv(os.Environ(), map[string]string{"GOHMOS_HOME": "", "GOOS": "openharmony", "GOARCH": "arm64", "GOROOT": "/deliberately-wrong", "GOTOOLCHAIN": "go99.0.0+auto"})
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("installed launcher %v: %s %v", args, output, err)
			}
			if args[0] == "env" && strings.TrimSpace(string(output)) != "openharmony" {
				t.Fatalf("target lost: %s", output)
			}
		}
	}
}

func TestGitEnvironmentIsolation(t *testing.T) {
	env := isolatedGitEnv([]string{"PATH=/bin", "HTTPS_PROXY=http://proxy.invalid", "GIT_TEMPLATE_DIR=/unrelated/templates", "GIT_DIR=/unrelated/.git", "GIT_WORK_TREE=/unrelated", "GIT_INDEX_FILE=/unrelated/index", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.bare", "GIT_CONFIG_VALUE_0=true"})
	joined := strings.Join(env, "\n")
	for _, bad := range []string{"GIT_TEMPLATE_DIR=", "GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_CONFIG_"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("leaked %s", bad)
		}
	}
	for _, want := range []string{"HTTPS_PROXY=http://proxy.invalid", "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestInstalledLauncher(t *testing.T) {
	home := filepath.Join(t.TempDir(), "custom SDK with spaces")
	root := cachedToolchain(t, home, testCommit)
	b, _ := json.Marshal(installation{Commit: testCommit, Host: host()})
	if err := atomicWrite(filepath.Join(home, "current.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, "bin", executable("gohmos"))
	if err := os.MkdirAll(filepath.Dir(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", executable("go")), "build", "-buildvcs=false", "-o", launcher, "../..")
	cmd.Env = replaceEnv(os.Environ(), map[string]string{"GOTOOLCHAIN": "local"})
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build launcher: %s %v", output, err)
	}
	env := replaceEnv(os.Environ(), map[string]string{"GOHMOS_HOME": ""})
	for _, check := range []struct{ arg, want string }{{"root", root}, {"--version", "gohmos " + installerVersion}} {
		cmd = exec.Command(launcher, check.arg)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), check.want) {
			t.Fatalf("launcher %s: %s %v", check.arg, output, err)
		}
	}
}

func TestBootstrapFallback(t *testing.T) {
	t.Setenv("GOROOT_BOOTSTRAP", "")
	t.Setenv("PATH", filepath.Join(runtime.GOROOT(), "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := discoverBootstrap(filepath.Join(t.TempDir(), "removed compiler"))
	if err != nil || got != runtime.GOROOT() {
		t.Fatalf("bootstrap fallback: %q %v", got, err)
	}
}

func TestPinnedSource(t *testing.T) {
	if sourceCommit != "50a9db343e53ec569374c328998b3ffbada8f850" {
		t.Fatal("legacy source pin changed")
	}
	if !fullCommit.MatchString(sourceCommit) {
		t.Fatalf("installer source pin is not a full commit: %q", sourceCommit)
	}
}

func TestUpgradeAndRollback(t *testing.T) {
	home := filepath.Join(t.TempDir(), "SDK with spaces")
	secondCommit := "2222222222222222222222222222222222222222"
	cachedToolchain(t, home, testCommit)
	cachedToolchain(t, home, secondCommit)
	i := installer{home: home, repository: "must not be contacted", out: io.Discard}
	for _, commit := range []string{testCommit, secondCommit, testCommit} {
		i.commit = commit
		if _, err := i.install(); err != nil {
			t.Fatal(err)
		}
		got, err := selectedRoot(home)
		if err != nil || got != rootPath(home, commit) {
			t.Fatalf("select %s: %q %v", commit, got, err)
		}
	}
	for _, commit := range []string{testCommit, secondCommit} {
		if !complete(rootPath(home, commit), commit) {
			t.Fatalf("selection discarded completed toolchain %s", commit)
		}
	}
}
