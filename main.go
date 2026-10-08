// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gohmos installs and runs an isolated, pinned OpenHarmony Go toolchain.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const repository = "https://github.com/ZxillyFork/go-hmos.git"

// sourceCommit is a complete, reviewed port commit, not a moving branch.
// Keep it separate from the commit that adds this installer to avoid self-reference.
const sourceCommit = "fcaf70eef17b74d6acb8fd220d80edab8fdf3799"
const installerVersion = "0.1.0-dev"
const minimumBootstrap = "go1.24.6"

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

type installation struct {
	Commit string `json:"commit"`
	Host   string `json:"host"`
}

type installer struct {
	home       string
	repository string
	commit     string
	bootstrap  string
	out        io.Writer
	launcher   bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gohmos:", err)
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	home, err := installHome()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(`gohmos installs and runs the experimental OpenHarmony Go fork.

  gohmos install [--home DIR]   Build and select this installer's pinned source
  gohmos download [--home DIR]  Alias for install (like gotip download)
  gohmos root                  Print the selected toolchain directory
  gohmos --version             Print installer and pinned-source versions
  gohmos <go arguments>        Run the selected Go with GOTOOLCHAIN=local

Requires Git and Go >= 1.24.6 for installation; bash on Linux/macOS.
The first install compiles Go and may take several minutes.
GOHMOS_HOME overrides ~/sdk/go-hmos. No PATH or global Go settings are changed.
To upgrade, run a newer pinned version of the installer with "install".
`)
		return nil
	}
	switch args[0] {
	case "--version":
		fmt.Printf("gohmos %s; source %s\n", installerVersion, sourceCommit)
		return nil
	case "install", "download":
		flags := flag.NewFlagSet("install", flag.ContinueOnError)
		flags.StringVar(&home, "home", home, "isolated installation directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("install accepts no positional arguments")
		}
		home, err = filepath.Abs(home)
		if err != nil {
			return err
		}
		i := installer{home: home, repository: repository, commit: sourceCommit, bootstrap: runtime.GOROOT(), out: os.Stderr, launcher: true}
		root, err := i.install()
		if err != nil {
			return err
		}
		launcher := filepath.Join(home, "bin", executable("gohmos"))
		fmt.Fprintf(os.Stderr, "\nInstalled %s\nRun: %s version\n", root, quoteCommand(launcher))
		fmt.Fprintln(os.Stderr, "OpenHarmony cross-linking still requires its official native SDK; see https://github.com/ZxillyFork/go-hmos-installer#openharmony-sdk-is-still-required.")
		return nil
	case "root":
		if len(args) != 1 {
			return errors.New("root accepts no arguments")
		}
		root, err := selectedRoot(home)
		if err != nil {
			return err
		}
		fmt.Println(root)
		return nil
	default:
		root, err := selectedRoot(home)
		if err != nil {
			return err
		}
		cmd := exec.Command(filepath.Join(root, "bin", executable("go")), args...)
		cmd.Env = replaceEnv(os.Environ(), map[string]string{"GOROOT": root, "GOTOOLCHAIN": "local"})
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
}

func installHome() (string, error) {
	if home := os.Getenv("GOHMOS_HOME"); home != "" {
		return filepath.Abs(home)
	}
	// An installed launcher remains usable when --home was used without setting
	// GOHMOS_HOME. A go run temporary executable does not satisfy this layout.
	if exe, err := os.Executable(); err == nil && filepath.Base(filepath.Dir(exe)) == "bin" && filepath.Base(exe) == executable("gohmos") {
		candidate := filepath.Dir(filepath.Dir(exe))
		if _, err := os.Stat(filepath.Join(candidate, "current.json")); err == nil {
			return candidate, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sdk", "go-hmos"), nil
}

func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func host() string { return runtime.GOOS + "-" + runtime.GOARCH }

func rootPath(home, commit string) string {
	return filepath.Join(home, "toolchains", commit+"-"+host())
}

func selectedRoot(home string) (string, error) {
	b, err := os.ReadFile(filepath.Join(home, "current.json"))
	if err != nil {
		return "", fmt.Errorf("no selected installation in %s; run gohmos install first: %w", home, err)
	}
	var current installation
	if err := json.Unmarshal(b, &current); err != nil {
		return "", err
	}
	if !fullCommit.MatchString(current.Commit) || current.Host != host() {
		return "", errors.New("invalid installation metadata or different host; rerun install")
	}
	root := rootPath(home, current.Commit)
	if !complete(root, current.Commit) {
		return "", errors.New("selected toolchain is incomplete; rerun install")
	}
	return root, nil
}

func complete(root, commit string) bool {
	b, err := os.ReadFile(filepath.Join(root, ".gohmos-complete"))
	if err != nil || string(b) != commit+"\n" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, "bin", executable("go")))
	return err == nil && info.Mode().IsRegular()
}

func (i installer) install() (string, error) {
	if !fullCommit.MatchString(i.commit) {
		return "", errors.New("installer has no valid pinned source commit")
	}
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		return "", fmt.Errorf("installation on %s is not supported", runtime.GOOS)
	}
	if err := os.MkdirAll(i.home, 0700); err != nil {
		return "", err
	}
	lock := filepath.Join(i.home, ".install-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		return "", fmt.Errorf("cannot acquire installation lock %s; if an interrupted install left this empty directory, remove it only after checking no installer is running: %w", lock, err)
	}
	defer os.Remove(lock)
	root := rootPath(i.home, i.commit)
	if !complete(root, i.commit) {
		if _, err := os.Stat(root); err == nil {
			return "", fmt.Errorf("refusing to overwrite incomplete directory %s; move it aside and retry", root)
		}
		if err := i.build(root); err != nil {
			return "", err
		}
	} else {
		fmt.Fprintln(i.out, "Using cached toolchain", i.commit)
	}
	if i.launcher {
		if _, err := installLauncher(i.home); err != nil {
			return "", fmt.Errorf("toolchain built at %s, but launcher was not installed; previous selection remains unchanged: %w", root, err)
		}
	}
	data, _ := json.MarshalIndent(installation{Commit: i.commit, Host: host()}, "", "  ")
	if err := atomicWrite(filepath.Join(i.home, "current.json"), append(data, '\n'), 0600); err != nil {
		return "", err
	}
	return root, nil
}

func (i installer) build(root string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("Git is required; install Git and retry")
	}
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("bash"); err != nil {
			return errors.New("bash is required to build Go on this host")
		}
	}
	bootstrap, err := discoverBootstrap(i.bootstrap)
	if err != nil {
		return err
	}
	i.bootstrap = bootstrap
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	env := cleanBuildEnv(os.Environ(), i.bootstrap, filepath.Join(i.home, "cache"))
	// Avoid user Git templates/hooks and interactive credential prompts. The
	// repository is public and no tokens, credentials, or user Git config are saved.
	gitEnv := isolatedGitEnv(env)
	git := func(args ...string) error {
		prefix := []string{"--git-dir=" + filepath.Join(stage, ".git"), "--work-tree=" + stage, "-c", "init.templateDir=", "-c", "core.hooksPath=" + filepath.Join(stage, ".no-hooks")}
		return command(stage, gitEnv, i.out, "git", append(prefix, args...)...)
	}
	fmt.Fprintln(i.out, "Fetching pinned source", i.commit)
	if err := git("init", "--quiet"); err != nil {
		return err
	}
	if err := git("remote", "add", "origin", i.repository); err != nil {
		return err
	}
	if err := git("fetch", "--quiet", "--depth=1", "origin", i.commit); err != nil {
		return err
	}
	check := exec.Command("git", "--git-dir="+filepath.Join(stage, ".git"), "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	check.Dir, check.Env = stage, gitEnv
	actual, err := check.Output()
	if err != nil || strings.TrimSpace(string(actual)) != i.commit {
		return errors.New("downloaded source commit does not match the pinned commit")
	}
	if err := git("checkout", "--quiet", "--detach", i.commit); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(stage, "src", "internal", "goos", "zgoos_openharmony.go")); err != nil {
		return errors.New("pinned source is missing the OpenHarmony port")
	}
	fmt.Fprintln(i.out, "Building host toolchain (first install can take several minutes)...")
	buildDir := filepath.Join(stage, "src")
	if runtime.GOOS == "windows" {
		err = command(buildDir, env, i.out, "cmd.exe", "/d", "/c", "make.bat")
	} else {
		err = command(buildDir, env, i.out, "bash", "make.bash")
	}
	if err != nil {
		return fmt.Errorf("build failed; previous installation remains selected: %w", err)
	}
	verify := exec.Command(filepath.Join(stage, "bin", executable("go")), "tool", "dist", "list")
	verify.Env = replaceEnv(env, map[string]string{"GOROOT": stage})
	targets, err := verify.Output()
	if err != nil || !bytes.Contains(targets, []byte("openharmony/arm64\n")) || !bytes.Contains(targets, []byte("openharmony/amd64\n")) {
		return errors.New("built toolchain did not report both OpenHarmony targets")
	}
	if err := os.WriteFile(filepath.Join(stage, ".gohmos-complete"), []byte(i.commit+"\n"), 0600); err != nil {
		return err
	}
	// Same filesystem rename: failed builds never replace a working toolchain.
	return os.Rename(stage, root)
}

func command(dir string, env []string, out io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, out, out
	return cmd.Run()
}

func cleanBuildEnv(env []string, bootstrap, cache string) []string {
	var clean []string
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		// Target SDK/compiler and user go env settings must not leak into the host
		// bootstrap. Runtime forwarding intentionally preserves these target settings.
		if strings.HasPrefix(upper, "GO") || strings.HasPrefix(upper, "CGO") || upper == "CC" || upper == "CXX" || strings.HasPrefix(upper, "CC_FOR_") || strings.HasPrefix(upper, "CXX_FOR_") {
			continue
		}
		clean = append(clean, item)
	}
	values := map[string]string{"GOROOT_BOOTSTRAP": bootstrap, "GOENV": "off", "GOTOOLCHAIN": "local", "CGO_ENABLED": "0", "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH, "GOHOSTOS": runtime.GOOS, "GOHOSTARCH": runtime.GOARCH, "GOFLAGS": "-p=4", "GOMAXPROCS": "4"}
	if cache != "" {
		values["GOCACHE"] = cache
	}
	return isolatedGitEnv(replaceEnv(clean, values))
}

func isolatedGitEnv(env []string) []string {
	var clean []string
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		// A surrounding git hook or exported worktree must never redirect this
		// installer into another repository. Keep ordinary proxy/transport setup.
		switch strings.ToUpper(key) {
		case "GIT_TEMPLATE_DIR", "GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL":
			continue
		}
		if strings.HasPrefix(strings.ToUpper(key), "GIT_CONFIG_KEY_") || strings.HasPrefix(strings.ToUpper(key), "GIT_CONFIG_VALUE_") {
			continue
		}
		clean = append(clean, item)
	}
	return replaceEnv(clean, map[string]string{"GIT_TERMINAL_PROMPT": "0"})
}

func replaceEnv(env []string, values map[string]string) []string {
	result := make([]string, 0, len(env)+len(values))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		replaced := false
		for k := range values {
			if strings.EqualFold(key, k) {
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, item)
		}
	}
	for k, v := range values {
		result = append(result, k+"="+v)
	}
	return result
}

// A launcher can outlive the Go installation that compiled it, and -trimpath
// can leave runtime.GOROOT empty. Fall back to a usable Go on PATH.
func discoverBootstrap(initial string) (string, error) {
	var candidates []string
	if explicit := os.Getenv("GOROOT_BOOTSTRAP"); explicit != "" {
		candidates = append(candidates, filepath.Join(explicit, "bin", executable("go")))
	}
	if initial != "" {
		candidates = append(candidates, filepath.Join(initial, "bin", executable("go")))
	}
	if goexe, err := exec.LookPath("go"); err == nil {
		candidates = append(candidates, goexe)
	}
	for _, goexe := range candidates {
		cmd := exec.Command(goexe, "env", "GOROOT", "GOVERSION")
		cmd.Env = cleanBuildEnv(os.Environ(), "", "")
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) != 2 || !bootstrapVersionOK(strings.TrimSpace(lines[1])) {
			continue
		}
		root := strings.TrimSpace(lines[0])
		if root == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, "bin", executable("go"))); err == nil && info.Mode().IsRegular() {
			return root, nil
		}
	}
	return "", errors.New("bootstrap Go >= 1.24.6 is required; install a current official Go on PATH or set GOROOT_BOOTSTRAP")
}

func bootstrapVersionOK(v string) bool {
	return version.IsValid(v) && version.Compare(v, minimumBootstrap) >= 0
}

func atomicWrite(name string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(name), ".gohmos-write-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), name)
}

func installLauncher(home string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(bin, executable("gohmos"))
	// Windows cannot replace a running executable. An unchanged launcher does
	// not need replacing, including when it invokes its own install command.
	if current, err := os.ReadFile(target); err == nil && sha256.Sum256(current) == sha256.Sum256(data) {
		return target, nil
	}
	if err := atomicWrite(target, data, 0755); err != nil {
		return "", err
	}
	return target, nil
}

func quoteCommand(path string) string {
	escaped := strings.ReplaceAll(path, "'", "'\\''")
	if runtime.GOOS == "windows" {
		return "& '" + strings.ReplaceAll(path, "'", "''") + "'"
	}
	return "'" + escaped + "'"
}
