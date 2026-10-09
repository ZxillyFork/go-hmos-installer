// Copyright 2026 The go-hmos Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package toolchain

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestVersionTerminalSignals(t *testing.T) {
	bin := installCommands(t)
	base := t.TempDir()
	root := cachedToolchain(t, filepath.Join(base, testVersion.Name), testVersion.Commit)
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
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGQUIT} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(bin, executable(testVersion.Name)), "wait-for-signal")
			cmd.Env = replaceEnv(os.Environ(), map[string]string{"GOHMOS_SDK_HOME": base, "GOHMOS_TEST_FAKE_GO": "1"})
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if ready, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || ready != "ready\n" {
				t.Fatalf("child readiness: %q %v", ready, err)
			}
			if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil || err.(*exec.ExitError).ExitCode() != 23 {
				t.Fatalf("signal exit: %v; stderr %s", err, stderr.String())
			}
			if stderr.String() != "child signal" {
				t.Fatalf("wrapper leaked signal output: %q", stderr.String())
			}
		})
	}
}
