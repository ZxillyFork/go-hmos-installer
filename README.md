# go-hmos installer

A small, isolated installer and `gohmos` launcher for the experimental
[OpenHarmony Go fork](https://github.com/ZxillyFork/go-hmos).
It fetches a pinned source commit and builds it on your development computer,
without replacing your regular Go installation.

There are currently **no prebuilt go-hmos releases**. The first install compiles
Go and takes several minutes. This is an experimental toolchain, not a claim of
commercial HarmonyOS NEXT device compatibility.

## One-command installation

Install [official Go](https://go.dev/dl/) **1.24.6 or later** and
[Git](https://git-scm.com/downloads) first. Linux/macOS also need `bash`.
No host C compiler is required for this installer. Run this same command in a
Linux/macOS terminal or Windows PowerShell:

```text
go run github.com/ZxillyFork/go-hmos-installer@latest install
```

Go downloads and verifies the installer using its normal module/checksum
mechanisms. The installer then builds the full 40-character source commit
embedded in `main.go`, never a moving source branch. For a reproducible installer
revision, replace `latest` with a reviewed full **installer commit SHA** from
this repository. Updating the installer does not modify an existing toolchain
unless you run its `install` command.

The command prints the exact path of the installed `gohmos` launcher. Normally:

```sh
# Linux/macOS
"$HOME/sdk/go-hmos/bin/gohmos" version
"$HOME/sdk/go-hmos/bin/gohmos" tool dist list
```

```powershell
# Windows PowerShell
& "$HOME/sdk/go-hmos/bin/gohmos.exe" version
& "$HOME/sdk/go-hmos/bin/gohmos.exe" tool dist list
```

For a custom location, append `--home "/path/with spaces/go-hmos"` to `install`.
The installed launcher finds its configuration beside its `bin` directory, so
no permanent environment variables are needed. `GOHMOS_HOME` can override it.
You may add the printed `bin` directory to PATH yourself if desired. Installation
never modifies PATH, replaces a command named `go`, or runs `go env -w`.

## Use and upgrade

`gohmos` forwards ordinary Go arguments, preserving target settings such as
`GOOS`, `GOARCH`, `CGO_ENABLED`, and `CC`. It sets `GOROOT` to the selected fork
and `GOTOOLCHAIN=local` for that process so Go cannot silently switch to an
upstream toolchain that lacks this port. `gohmos root` prints the selected source
and toolchain directory. `gohmos --version` identifies the installer/source pin;
`gohmos version` identifies the built Go toolchain.

To upgrade, rerun the one-line installation command (or use a newer reviewed
installer commit). Repeating a revision reuses its completed build. The installed
`gohmos install` or `gohmos download` selects the source pinned by that launcher;
it does not fetch a newer installer. Existing completed toolchains remain
available. To roll back, rerun an earlier installer revision. No updates happen
in the background.

During host bootstrap, target/compiler/Go configuration is isolated, cgo is
disabled for the host build, and build concurrency is limited to four. A source
fetch or build failure leaves the previously selected toolchain usable. Builds
happen in a temporary sibling directory and are selected only after both
OpenHarmony targets are verified. Concurrent installs fail with a lock message.
If a forcibly terminated process leaves an empty `.install-lock` directory,
check that no installer is running, remove that empty directory, then retry.
Unfinished `.build-*` directories can be removed when no installer is running.

The source revision is verified against Git's fetched commit ID. This pins
content; it is not a code-signing or device-compatibility guarantee. Git uses
normal HTTPS and your existing network/proxy configuration. No token is needed
for the public repository, and no credentials are saved by this installer.

## OpenHarmony SDK is still required

Installation produces **host** Go tools. Cross-linking OpenHarmony executables
or native libraries still requires the official SDK's matching Clang and
sysroot, `CGO_ENABLED=1`, and external linking. The target is
`GOOS=openharmony` with `GOARCH=arm64` or `amd64`; the SDK triples are
`aarch64-linux-ohos` and `x86_64-linux-ohos`.

Read [the port documentation](https://github.com/ZxillyFork/go-hmos/blob/50a9db343e53ec569374c328998b3ffbada8f850/doc/openharmony.md)
and [its implementation and verification status](https://github.com/ZxillyFork/go-hmos/pull/1)
before building for devices. This installer does not download the SDK, deploy to
a phone, or establish commercial HarmonyOS NEXT compatibility.

## Verification

```sh
go test ./...
go vet ./...
# Optional source bootstrap (uses the installer's fixed sourceCommit):
GOHMOS_TEST_REPOSITORY=https://github.com/ZxillyFork/go-hmos.git \
  go test -run '^TestRealInstallation$' -timeout 25m -v
# For a local checkout, supply its path and GOHMOS_TEST_COMMIT=<full-commit-sha>.
```

This repository's CI runs unit tests and source installation on Linux, macOS,
and Windows. The source installation test uses directories containing spaces,
checks a cached reinstall, compiles/runs a host smoke program, and exercises the
installed launcher. Host tests do not substitute for OpenHarmony SDK linking or
device execution.
