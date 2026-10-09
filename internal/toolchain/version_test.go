// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package toolchain

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testVersion = Version{Name: "go1.27.2", Commit: "b637b8617624655906b737977f50de5280bf7f65", GOVersion: "go1.27.2-hmos-devel"}

func TestVersionHomes(t *testing.T) {
	base := t.TempDir()
	t.Setenv("GOHMOS_HOME", filepath.Join(base, "legacy"))
	t.Setenv("GOHMOS_SDK_HOME", "")
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	got, err := versionHome(testVersion.Name)
	if err != nil || got != filepath.Join(base, "sdk", testVersion.Name+"-hmos") {
		t.Fatalf("default home = %q, %v", got, err)
	}
	t.Setenv("GOHMOS_SDK_HOME", filepath.Join(base, "custom SDK"))
	for _, name := range []string{testVersion.Name, "go1.27.3"} {
		got, err := versionHome(name)
		if err != nil || got != filepath.Join(base, "custom SDK", name+"-hmos") {
			t.Fatalf("custom home = %q, %v", got, err)
		}
	}
}

func TestVersionRejectsSourceSelection(t *testing.T) {
	t.Setenv("GOHMOS_SDK_HOME", t.TempDir())
	for _, args := range [][]string{{"download", "main"}, {"download", testCommit}, {"download", "--home", "/tmp/elsewhere"}} {
		if err := testVersion.run(args); err == nil || !strings.Contains(err.Error(), "no branch, commit, or arguments") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	bad := testVersion
	bad.Name = "../go-hmos"
	if err := bad.run([]string{"download"}); err == nil {
		t.Fatal("accepted path traversal in compiled configuration")
	}
	for _, args := range [][]string{nil, {"version"}, {"install"}, {"help"}} {
		if err := testVersion.run(args); err == nil || !strings.Contains(err.Error(), testVersion.Name+" download") {
			t.Fatalf("missing download %v: %v", args, err)
		}
	}
}

func TestPinnedCacheDoesNotSelect(t *testing.T) {
	home := t.TempDir()
	cachedToolchain(t, home, testVersion.Commit)
	old := []byte(`{"commit":"` + testCommit + `","host":"` + host() + `"}`)
	if err := os.WriteFile(filepath.Join(home, "current.json"), old, 0600); err != nil {
		t.Fatal(err)
	}
	i := installer{home: home, repository: "must not be contacted", commit: testVersion.Commit, pinned: true, out: io.Discard}
	if _, err := i.install(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, "current.json"))
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("altered mutable selection: %s %v", got, err)
	}
}

type goInvocation struct {
	Args  []string
	Env   map[string]string
	Input string
}

// A copy of this test executable is the fake toolchain in subprocess tests.
// This mode is compiled only into tests, never into either public command.
func init() {
	if os.Getenv("GOHMOS_TEST_FAKE_GO") != "1" {
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "wait-for-signal" {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, wrapperSignals...)
		fmt.Fprintln(os.Stdout, "ready")
		<-ch
		fmt.Fprint(os.Stderr, "child signal")
		os.Exit(23)
	}
	input, _ := io.ReadAll(os.Stdin)
	env := make(map[string]string)
	for _, key := range []string{"GOROOT", "GOTOOLCHAIN", "GOOS", "GOARCH", "CGO_ENABLED", "CC", "PATH"} {
		env[key] = os.Getenv(key)
	}
	_ = json.NewEncoder(os.Stdout).Encode(goInvocation{Args: os.Args[1:], Env: env, Input: string(input)})
	fmt.Fprint(os.Stderr, "fixture stderr")
	if len(os.Args) > 1 && os.Args[1] == "fail" {
		os.Exit(23)
	}
	os.Exit(0)
}

// Exercise the exact @latest install form against a local module proxy. This
// validates package naming/module resolution without publishing a commit or
// depending on the public repository already containing the new package.
func installCommands(t *testing.T) string {
	t.Helper()
	const module = "github.com/ZxillyFork/go-hmos-installer"
	const version = "v0.0.0-20261009000000-000000000000"
	base := t.TempDir()
	proxy := filepath.Join(base, "proxy")
	versions := filepath.Join(proxy, "github.com", "!zxilly!fork", "go-hmos-installer", "@v")
	if err := os.MkdirAll(versions, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"list": version + "\n", version + ".info": `{"Version":"` + version + `","Time":"2026-10-09T00:00:00Z"}`, version + ".mod": "module " + module + "\n\ngo 1.24.6\n"} {
		if err := os.WriteFile(filepath.Join(versions, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := os.Create(filepath.Join(versions, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(archive)
	err = filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !(strings.HasSuffix(path, ".go") || entry.Name() == "go.mod" || entry.Name() == "LICENSE") {
			return nil
		}
		rel, err := filepath.Rel("../..", path)
		if err != nil {
			return err
		}
		out, err := zw.Create(module + "@" + version + "/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(base, "bin")
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}
	if runtime.GOOS == "windows" {
		u.Path = "/" + u.Path
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", executable("go")), "install", module+"@latest", module+"/"+testVersion.Name+"@latest")
	cmd.Env = replaceEnv(os.Environ(), map[string]string{"GOTOOLCHAIN": "local", "GOWORK": "off", "GOENV": "off", "GOFLAGS": "-modcacherw", "GOSUMDB": "off", "GONOSUMDB": "", "GONOPROXY": "", "GOPRIVATE": "", "GOPROXY": u.String(), "GOBIN": bin, "GOMODCACHE": filepath.Join(base, "modcache")})
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go install @latest through local proxy: %s %v", output, err)
	}
	return bin
}

func TestInstalledVersionCommand(t *testing.T) {
	bin := installCommands(t)
	launcher := filepath.Join(bin, executable(testVersion.Name))
	legacy := filepath.Join(bin, executable("go-hmos-installer"))
	for _, check := range []struct{ exe, want string }{{legacy, sourceCommit}, {launcher, testVersion.Commit}} {
		out, err := exec.Command(check.exe, "--version").CombinedOutput()
		if err != nil || !strings.Contains(string(out), check.want) {
			t.Fatalf("%s --version: %s %v", check.exe, out, err)
		}
	}
	userHome := t.TempDir()
	base := filepath.Join(userHome, "sdk")
	officialHome := filepath.Join(base, testVersion.Name)
	if err := os.MkdirAll(officialHome, 0700); err != nil {
		t.Fatal(err)
	}
	officialMarker := filepath.Join(officialHome, ".unpacked-success")
	if err := os.WriteFile(officialMarker, []byte("official SDK must stay untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, testVersion.Name+"-hmos")
	root := cachedToolchain(t, home, testVersion.Commit)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", executable("go")), data, 0755); err != nil {
		t.Fatal(err)
	}
	legacyHome := filepath.Join(base, "legacy")
	cachedToolchain(t, legacyHome, testCommit)
	old := []byte(`{"commit":"` + testCommit + `","host":"` + host() + `"}`)
	if err := os.WriteFile(filepath.Join(legacyHome, "current.json"), old, 0600); err != nil {
		t.Fatal(err)
	}
	env := replaceEnv(os.Environ(), map[string]string{"HOME": userHome, "USERPROFILE": userHome, "GOHMOS_SDK_HOME": "", "GOHMOS_HOME": legacyHome, "GOHMOS_TEST_FAKE_GO": "1", "GOROOT": "/wrong root", "GOTOOLCHAIN": "go99.0.0+auto", "GOOS": "openharmony", "GOARCH": "arm64", "CGO_ENABLED": "1", "CC": "target clang with spaces"})
	for n := 0; n < 2; n++ {
		cmd := exec.Command(launcher, "download")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "Using cached toolchain "+testVersion.Commit) {
			t.Fatalf("download: %s %v", out, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "current.json")); !os.IsNotExist(err) {
		t.Fatalf("version created mutable selection: %v", err)
	}
	// Even a bogus mutable selector must not redirect the versioned command.
	if err := os.WriteFile(filepath.Join(home, "current.json"), old, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"help"}, {"install", "example.org/tool@v1.2.3"}, {"env", "GOROOT"}, {"run", "a file.go", "argument with spaces"}} {
		cmd := exec.Command(launcher, args...)
		cmd.Env = env
		cmd.Stdin = strings.NewReader("standard input")
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("forward %v: %v %s", args, err, stderr.String())
		}
		var got goInvocation
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if strings.Join(got.Args, "\x00") != strings.Join(args, "\x00") || got.Input != "standard input" || stderr.String() != "fixture stderr" {
			t.Fatalf("forwarded %v: %+v, stderr %q", args, got, stderr.String())
		}
		for key, want := range map[string]string{"GOROOT": root, "GOTOOLCHAIN": "local", "GOOS": "openharmony", "GOARCH": "arm64", "CGO_ENABLED": "1", "CC": "target clang with spaces"} {
			if got.Env[key] != want {
				t.Fatalf("%s=%q, want %q", key, got.Env[key], want)
			}
		}
		if !strings.HasPrefix(got.Env["PATH"], filepath.Join(root, "bin")+string(os.PathListSeparator)) {
			t.Fatalf("nested Go missing from PATH: %q", got.Env["PATH"])
		}
	}
	cmd := exec.Command(launcher, "fail")
	cmd.Env = env
	var failureStderr bytes.Buffer
	cmd.Stderr = &failureStderr
	if err := cmd.Run(); err == nil || err.(*exec.ExitError).ExitCode() != 23 {
		t.Fatalf("exit code: %v", err)
	}
	if failureStderr.String() != "fixture stderr" {
		t.Fatalf("wrapper added error output: %q", failureStderr.String())
	}
	if got, err := os.ReadFile(officialMarker); err != nil || string(got) != "official SDK must stay untouched" {
		t.Fatalf("official SDK changed: %q %v", got, err)
	}
	entries, err := os.ReadDir(officialHome)
	if err != nil || len(entries) != 1 {
		t.Fatalf("official SDK contents changed: %v %v", entries, err)
	}
	got, err := os.ReadFile(filepath.Join(legacyHome, "current.json"))
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("legacy changed: %s %v", got, err)
	}
}

// Explicit opt-in: build the new command's exact source pin, then exercise the
// installed command's cached download, relocation, host run, and security tests.
func TestRealVersionInstallation(t *testing.T) {
	repository := os.Getenv("GOHMOS_TEST_REPOSITORY")
	if repository == "" {
		t.Skip("set GOHMOS_TEST_REPOSITORY to run the pinned source bootstrap")
	}
	bin := installCommands(t)
	launcher := filepath.Join(bin, executable(testVersion.Name))
	base := filepath.Join(t.TempDir(), "versioned SDK with spaces")
	home := filepath.Join(base, testVersion.Name+"-hmos")
	t.Setenv("GIT_DIR", filepath.Join(base, "unrelated repo"))
	t.Setenv("GIT_WORK_TREE", filepath.Join(base, "unrelated worktree"))
	t.Setenv("GOOS", "openharmony")
	t.Setenv("GOARCH", "arm64")
	t.Setenv("CC", "nonexistent target compiler")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GOTOOLCHAIN", "go99.0.0+auto")
	i := installer{home: home, repository: repository, commit: testVersion.Commit, version: testVersion.GOVersion, bootstrap: runtime.GOROOT(), out: os.Stderr, pinned: true}
	root, err := i.install()
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(filepath.Join(root, "bin", executable("go")))
	if err != nil {
		t.Fatal(err)
	}
	env := replaceEnv(cleanBuildEnv(os.Environ(), runtime.GOROOT(), filepath.Join(home, "cache")), map[string]string{"GOHMOS_SDK_HOME": base, "GOHMOS_HOME": filepath.Join(base, "legacy must not be used"), "GOROOT": "/deliberately-wrong", "GOTOOLCHAIN": "go99.0.0+auto"})
	cmd := exec.Command(launcher, "download")
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("Using cached toolchain "+testVersion.Commit)) {
		t.Fatalf("cached public download: %s %v", output, err)
	}
	second, err := os.Stat(filepath.Join(root, "bin", executable("go")))
	if err != nil || !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("cached public download rebuilt toolchain")
	}
	if _, err := os.Stat(filepath.Join(home, "current.json")); !os.IsNotExist(err) {
		t.Fatalf("wrote mutable selection: %v", err)
	}
	cmd = exec.Command(launcher, "env", "GOROOT", "GOVERSION", "GOTOOLCHAIN")
	cmd.Env = env
	output, err = cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(strings.ReplaceAll(string(output), "\r\n", "\n")) != root+"\n"+testVersion.GOVersion+"\nlocal" {
		t.Fatalf("relocated command: %s %v", output, err)
	}
	program := filepath.Join(home, "hello.go")
	if err := os.WriteFile(program, []byte("package main\nfunc main() { println(\"versioned host smoke passed\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(launcher, "run", program)
	cmd.Env = env
	output, err = cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("versioned host smoke passed")) {
		t.Fatalf("host smoke: %s %v", output, err)
	}
	cmd = exec.Command(launcher, "env", "GOOS", "GOARCH", "CGO_ENABLED", "CC")
	cmd.Env = replaceEnv(env, map[string]string{"GOOS": "openharmony", "GOARCH": "arm64", "CGO_ENABLED": "1", "CC": "target clang"})
	output, err = cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(strings.ReplaceAll(string(output), "\r\n", "\n")) != "openharmony\narm64\n1\ntarget clang" {
		t.Fatalf("target forwarding: %s %v", output, err)
	}
	// Run the installed fork's os.Root tests in its own std module. On Windows,
	// require explicit passes for CVE-2026-56857 dangling-junction cases.
	cmd = exec.Command(launcher, "test", "-json", "-count=1", "-timeout=10m", "os", "-run", "^TestRootMulti(Mkdir|MkdirAllShallow|MkdirAllDeep|Lstat|Open|OpenFile|Stat|ReadFile|Readlink)$")
	cmd.Dir = filepath.Join(root, "src")
	cmd.Env = replaceEnv(env, map[string]string{"GO111MODULE": "on", "GOWORK": "off"})
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("installed os.Root regressions: %s %v", output, err)
	}
	passed := make(map[string]bool)
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event struct{ Action, Test string }
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode os.Root event: %q: %v", line, err)
		}
		if event.Action == "pass" {
			passed[event.Test] = true
		}
	}
	for _, name := range []string{"TestRootMultiMkdir", "TestRootMultiMkdirAllShallow", "TestRootMultiMkdirAllDeep"} {
		if !passed[name] {
			t.Fatalf("os.Root regression did not pass: %s", name)
		}
		if runtime.GOOS == "windows" {
			for _, target := range []string{"bJbA", "bsJbA"} {
				if !passed[name+"/"+target] {
					t.Fatalf("Windows junction regression did not pass: %s/%s", name, target)
				}
				t.Logf("Windows junction regression passed: %s/%s", name, target)
			}
		}
	}
	t.Log("version command: source build, cached public download, relocation, host smoke, environment forwarding, and os.Root regressions passed")
}
