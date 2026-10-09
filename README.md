# go-hmos installer

Versioned commands for the experimental [OpenHarmony Go fork](https://github.com/ZxillyFork/go-hmos),
following the `golang.org/dl/go1.x` / `gotip` install-then-download pattern.
They build a fixed source commit on your development computer without replacing
your regular Go installation.

`go1.27.2` builds the pinned development snapshot
[`b637b8617624655906b737977f50de5280bf7f65`](https://github.com/ZxillyFork/go-hmos/commit/b637b8617624655906b737977f50de5280bf7f65).
The resulting toolchain identifies itself as **`go1.27.2-hmos-devel`**.
The command name is a version selector, not an official release tag. There is no
official go-hmos release or prebuilt binary for this snapshot. This remains an
experimental OpenHarmony port, with no claim of commercial HarmonyOS NEXT device
compatibility.

## Install a versioned command

Install [official Go](https://go.dev/dl/) **1.24.6 or later** and
[Git](https://git-scm.com/downloads) first. Linux/macOS also need `bash`.
No host C compiler is required for the installer. In a terminal or PowerShell:

```text
go install github.com/ZxillyFork/go-hmos-installer/go1.27.2@latest
go1.27.2 download
go1.27.2 version
```

The first command installs the small launcher in `GOBIN` (normally the `bin`
directory of `GOPATH`, usually `$HOME/go/bin`). That directory must be on PATH;
otherwise invoke the launcher by its full path.

**Launcher name collision:** the official `golang.org/dl/go1.27.2` wrapper also
installs a binary named `go1.27.2`. They cannot coexist in the same `GOBIN`;
installing one there replaces the other launcher. If you use both, choose a
separate `GOBIN` for this fork (for example `$HOME/go-hmos/bin`) and invoke it by
full path. The fork's SDK storage is separate: it uses `~/sdk/go1.27.2-hmos`,
never the official wrapper's `~/sdk/go1.27.2` directory.

For the default launcher location:

```sh
# Linux/macOS
"$HOME/go/bin/go1.27.2" download
"$HOME/go/bin/go1.27.2" version
```

```powershell
# Windows PowerShell
& "$HOME/go/bin/go1.27.2.exe" download
& "$HOME/go/bin/go1.27.2.exe" version
```

`download` fetches the fixed source and **compiles Go**, which can take several
minutes. It does not download a prebuilt SDK. Running it again reuses the
completed build. It accepts no branch, commit, CL number, or other arguments.

Go installs the launcher using its normal module/checksum mechanisms. To pin
that launcher revision too, replace `latest` with a reviewed full **installer
commit SHA** from this repository. `@latest` updates the launcher implementation;
the `go1.27.2` command remains tied to its embedded source commit. A future
toolchain version gets a separate command package and executable.

## Use and storage

Use the versioned command just like `go`, including `install`:

```text
go1.27.2 help
go1.27.2 env GOROOT GOVERSION
go1.27.2 tool dist list
go1.27.2 build ./...
go1.27.2 test ./...
go1.27.2 install example.org/tool@v1.2.3
```

The final example is a placeholder; substitute the package you want to install.
`download` and `--version` are launcher commands. `--version` prints the command,
experimental toolchain identity, and source commit without requiring a download.
All other arguments, including no arguments and `help`, go to the built Go tool.
Its stdin, stdout, stderr, and exit status are forwarded.

Each version has its own directory under `~/sdk`, for example
`~/sdk/go1.27.2-hmos/toolchains/<source-commit>-<host-os>-<host-arch>`.
Set `GOHMOS_SDK_HOME` to a different **parent** SDK directory if needed; each
command still gets its own `<command>-hmos` subdirectory. Use the same setting for
`download` and subsequent commands. A versioned command does not use the legacy
`GOHMOS_HOME` variable or `current.json` selection, and cannot select another
version's cached toolchain.

Target settings such as `GOOS`, `GOARCH`, `CGO_ENABLED`, and `CC` are preserved
when forwarding. For the child process, the launcher sets `GOROOT` to its pinned
toolchain, adds that toolchain's `bin` to the front of PATH for nested Go
commands, and sets `GOTOOLCHAIN=local`. This prevents automatic switching to a
stock Go toolchain that lacks the OpenHarmony port. Installation never changes
your persistent PATH, replaces a command named `go`, or runs `go env -w`.

During source bootstrap, target/compiler/Go configuration is isolated, cgo is
disabled, and build concurrency is limited to four. The build is completed in a
temporary sibling directory and atomically moved into place only after both
OpenHarmony targets and the expected toolchain identity are verified. Failed
builds leave completed toolchains intact. Concurrent downloads fail with a lock
message. If a forcibly terminated process leaves an empty `.install-lock`
directory, check that no installer is running before removing it and retrying.
Unfinished `.build-*` directories can be removed when no installer is running.

The source revision is verified against Git's fetched commit ID. This pins
content; it is not a code-signing or device-compatibility guarantee. Git uses
normal HTTPS and your existing network/proxy configuration. No token is needed
for the public repository, and no credentials are saved by this installer.

## Legacy root command

The original root entry remains available for compatibility:

```text
go run github.com/ZxillyFork/go-hmos-installer@latest install
```

It retains source commit `50a9db343e53ec569374c328998b3ffbada8f850`
(`go1.27.1-hmos-devel`), the `gohmos` launcher, and its original behavior:
`install` / `download`, `--home`, `GOHMOS_HOME`, `root`, `--version`, and the
selection in `~/sdk/go-hmos/current.json`. The installed launcher discovers a
custom home beside its own `bin` directory. It forwards other Go arguments with
`GOTOOLCHAIN=local`.

Installing or using `go1.27.2` does not upgrade that legacy toolchain or
change its selection. Use the new versioned command explicitly for the newer
source. Future versioned commands can coexist in the same launcher directory.

## OpenHarmony SDK is still required

Installation produces **host** Go tools. Cross-linking OpenHarmony executables
or native libraries still requires the official SDK's matching Clang and
sysroot, `CGO_ENABLED=1`, and external linking. The target is
`GOOS=openharmony` with `GOARCH=arm64` or `amd64`; the SDK triples are
`aarch64-linux-ohos` and `x86_64-linux-ohos`.

Read [the pinned port documentation](https://github.com/ZxillyFork/go-hmos/blob/b637b8617624655906b737977f50de5280bf7f65/doc/openharmony.md)
and [implementation and verification status](https://github.com/ZxillyFork/go-hmos/pull/1)
before building for devices. This installer does not download the SDK, deploy to
a phone, or establish commercial HarmonyOS NEXT compatibility.

## Verification

```sh
go test ./...
go vet ./...
# Optional fresh builds of both fixed source versions:
GOHMOS_TEST_REPOSITORY=https://github.com/ZxillyFork/go-hmos.git \
  go test ./internal/toolchain -run '^TestReal(Version)?Installation$' -timeout 65m -v
```

`GOHMOS_TEST_REPOSITORY` can instead point to a local checkout containing the
pinned commits. Tests build real command packages, including the exact
`go install ...@latest` form against an isolated local module proxy; nothing is
published by that test. Unit/subprocess coverage checks the legacy pin, separate
state, cached downloads, forwarding, environment isolation, and exit codes.

CI is configured for Linux, macOS, and Windows source builds, plus Go 1.24.6
compatibility. Each real install test checks paths with spaces, relocation, a
cached install, a host program, and the installed command. The new version's
test also checks its exact identity and runs the installed fork's selected
`os.Root` regressions in the standard library module. On Windows it requires
explicit passes for the dangling-junction cases covering CVE-2026-56857. Host
tests do not substitute for OpenHarmony SDK linking or device execution.
