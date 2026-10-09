// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package toolchain

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Version is a command's immutable source and toolchain identity. Its values
// are compiled into the versioned main; there is no user-selectable source ref.
type Version struct {
	Name      string
	Commit    string
	GOVersion string
}

var commandName = regexp.MustCompile(`^go[0-9]+\.[0-9]+\.[0-9]+-hmos$`)

// Main runs a versioned command, preserving Go's argument and exit behavior.
func Main(v Version) {
	if err := v.run(os.Args[1:]); err != nil {
		var exit *forwardedExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", v.Name, err)
		os.Exit(1)
	}
}

func (v Version) run(args []string) error {
	if !commandName.MatchString(v.Name) || !fullCommit.MatchString(v.Commit) || v.GOVersion != v.Name+"-devel" {
		return errors.New("invalid compiled-in version configuration")
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Printf("%s; experimental %s; source %s\n", v.Name, v.GOVersion, v.Commit)
		return nil
	}
	home, err := versionHome(v.Name)
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "download" {
		if len(args) != 1 {
			return fmt.Errorf("usage: %s download (no branch, commit, or arguments accepted)", v.Name)
		}
		i := installer{home: home, repository: repository, commit: v.Commit,
			bootstrap: runtime.GOROOT(), out: os.Stderr, pinned: true, version: v.GOVersion}
		root, err := i.install()
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\nInstalled experimental %s in %s\nRun: %s version\n", v.GOVersion, root, v.Name)
		fmt.Fprintln(os.Stderr, "OpenHarmony cross-linking still requires its official native SDK; see https://github.com/ZxillyFork/go-hmos-installer#openharmony-sdk-is-still-required.")
		return nil
	}
	// Never follow current.json or another command's selection, including when
	// the legacy GOHMOS_HOME is set. Every version owns its source pin and state.
	root := rootPath(home, v.Commit)
	if !complete(root, v.Commit) {
		return fmt.Errorf("not downloaded; run '%s download' to build the pinned toolchain in %s", v.Name, home)
	}
	cmd := exec.Command(filepath.Join(root, "bin", executable("go")), args...)
	cmd.Env = versionEnv(os.Environ(), root)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	stop := suppressWrapperSignals()
	defer stop()
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return &forwardedExitError{exit}
	}
	return err
}

// Keep ordinary Go failures quiet while retaining context for download errors.
type forwardedExitError struct{ *exec.ExitError }

func versionHome(name string) (string, error) {
	if base := os.Getenv("GOHMOS_SDK_HOME"); base != "" {
		return filepath.Abs(filepath.Join(base, name))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sdk", name), nil
}

func versionEnv(env []string, root string) []string {
	// Match golang.org/dl's PATH behavior for nested go run/test/generate
	// commands. Force local toolchain selection so this fork cannot be replaced
	// with an automatically downloaded stock toolchain lacking OpenHarmony.
	path := filepath.Join(root, "bin")
	for _, item := range env {
		key, value, found := strings.Cut(item, "=")
		if found && value != "" && strings.EqualFold(key, "PATH") {
			path += string(os.PathListSeparator) + value
			break
		}
	}
	return replaceEnv(env, map[string]string{"GOROOT": root, "GOTOOLCHAIN": "local", "PATH": path})
}
