# AGENTS.md

Instructions for AI coding agents working in this repository.

## Project overview

**ptt-fix** is a Linux utility that works around push-to-talk limitations under Wayland for X11 clients (for example Discord under XWayland).

Architecture:

1. **Listen** — read a configured key from one or more input devices via Linux evdev (`/dev/input`).
2. **Inject** — synthesize the configured key or mouse button through **X** (libxdo / XTest) so X11/XWayland apps receive it.

The X injection path is intentional. Kernel-level injection (uinput and similar) does not preserve this bridge: it routes through the compositor to the focused client, not through the X server path that unfocused X apps use for PTT. Do not replace libxdo/XTest with uinput unless the design goal changes.

## Technology stack

| Layer | Choice |
|-------|--------|
| Language | Go — see `go.mod` for the required toolchain |
| Module | `deedles.dev/ptt-fix` |
| Build | cgo; needs `libxdo` (pkg-config) at build time |
| Runtime | Linux input devices; X display (typically XWayland) for injection |

Do not pin toolchain or dependency versions in this file (they go stale). Prefer “as specified in `go.mod`” or unversioned names.

### Layout

| Path | Role |
|------|------|
| `ptt-fix.go` | Entry point, flags, config load, orchestration |
| `listen.go` | Per-device evdev listener and retry loop |
| `handle.go` | Event handling; key/mouse senders via xdo |
| `internal/evdev` | Pure-Go evdev (syscalls / ioctls; no cgo) |
| `internal/xdo` | Thin cgo wrapper around libxdo |
| `internal/config` | Config parse + embedded default config |
| `ptt-fix.service` | Example systemd unit |

## Development commands

```bash
go mod download
go test ./...
go vet ./...
go fmt ./...
```

`go test` already compiles packages; do not run a separate `go build` only to check that the project compiles. Building or installing still requires a C toolchain and libxdo development headers because of cgo.

## Code style and conventions

- **Logging** — `log/slog` with structured key-value fields.
- **Context** — pass `context.Context` as the first argument for cancelable / long-running work.
- **Errors** — handle explicitly; wrap with `fmt.Errorf("...: %w", err)` when adding context.
- **Modern Go** — match current stdlib helpers (`slices`, `maps`, `cmp`, `iter`, etc.) as used with the toolchain in `go.mod`.
- **Imports** — goimports-style groups: standard library, third-party, then `deedles.dev/...`.
- **Scope** — prefer small, focused changes. Do not reformat unrelated files or drive-by refactors.
- **cgo** — keep `internal/xdo` thin; avoid spreading cgo into other packages.

## Agent guidelines

1. **Git is read-only under all circumstances.** Never run write/mutating git commands. That includes (non-exhaustive): `commit`, `add`, `rm`, `mv`, `restore --staged`, `checkout`, `switch`, `branch` (create/delete), `merge`, `rebase`, `cherry-pick`, `stash`, `reset`, `clean`, `tag`, `push`, `pull` (when it updates refs), `am`, `revert`, `commit --amend`, or anything that modifies the index, working tree via git, or remote state. Read-only commands (`status`, `diff`, `log`, `show`, `blame`, `ls-files`, etc.) are fine. Leave all commits and branch management to the user.
2. **Do not pin versions in this file** — refer to `go.mod` or unversioned names so these instructions stay valid as versions change.
3. **Verify** with `go test ./...` and `go vet ./...` before considering work done.
4. **Preserve the X injection model** when changing input/output paths unless the user explicitly redesigns the bridge.
5. **Secrets** — do not commit tokens, API keys, or machine-specific paths.
