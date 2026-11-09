<!-- gsd-project-start source:PROJECT.md -->

## Project

**nfsense**

nfsense is an open-source firewall manager for a single Debian host running nftables. It gives operators a pfSense-inspired experience—top-level menus plus a scriptable CLI—while keeping all intended state in SQLite and applying changes through an explicit transactional pipeline (draft → check → apply → rollback). The web/PHP control plane is deliberately out of the picture; API/MCP can come later on the same model.

**Core Value:** Never lose control: dry-run, apply, and rollback without taking down SSH (or the management path).

### Constraints

- **OS**: Debian (v1) — package and test story stays focused
- **Firewall engine**: nftables only in v1 — no iptables-legacy primary path
- **Language**: Go — single static-ish binary, solid TUI ecosystem (e.g. Bubble Tea)
- **State**: SQLite for intended config and apply history — not “edit live nft and hope”
- **Privileges**: Applying policy requires appropriate privileges (root/capabilities); design must keep management path recoverable
- **Blast radius**: UI/CLI must not couple fatal failures into SSH the way a broken web stack did on pfSense
- **Backend seam**: Keep a thin `FirewallBackend` (or equivalent) boundary so a future `pf` driver is conceivable—unimplemented in v1

<!-- gsd-project-end -->

<!-- gsd-stack-start source:research/STACK.md -->

## Technology Stack

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Go | **1.26+** (toolchain **1.27.3** current on go.dev) | Language / single binary | Already decided; `golang.org/x/crypto` v0.58.0 requires Go ≥1.26. Pin `go 1.26` in `go.mod`; CI builds with latest stable. |
| nftables userspace (`nft`) | Debian package **`nftables`** (runtime dep) | Validate + atomic apply | `nft --check --file` is the documented dry-run; `nft --file` loads one atomic transaction. Matches Core Value (check → apply → rollback). |
| Compile-to-nft + thin `exec` wrapper | n/a (project code) | Primary `FirewallBackend` apply path | Shorewall-style: SQLite intended state → generated `.nft` → check/apply. Full expressiveness (filter+NAT+sets); no netlink impedance mismatch. |
| `github.com/google/nftables` | **v0.3.0** | Optional live introspection / owned-table sync | Pure-Go netlink; batches via `Flush()`. Use for **read/diff of owned tables**, not as sole apply path (no `nft --check` equivalent). |
| `github.com/spf13/cobra` | **v1.10.2** | CLI surface + TUI entry | De-facto Go CLI standard: subcommands, `PersistentFlags`, `RunE`. Root → TUI; `check`/`apply`/`rollback` stay scriptable. |
| Charm Bubble Tea **v2** | **v2.1.1** (`charm.land/bubbletea/v2`) | Full-screen operator TUI | Greenfield should take v2 now (declarative `tea.View`, module moved to `charm.land`). Matches Clipper-style menus. |
| SQLite via `modernc.org/sqlite` | **v1.62.0** | Source of truth + auth + apply history | Pure Go `database/sql` driver — no cgo → easy static binary + `.deb`. Driver name `"sqlite"`. |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `charm.land/lipgloss/v2` | **v2.0.6** | TUI styling / layout | Always with Bubble Tea menus and status panes. |
| `charm.land/bubbles/v2` | **v2.2.1** | Lists, viewports, text inputs | Table/rule browsers, scrollable logs. |
| `charm.land/huh/v2` | **v2.0.3** | Forms / wizards | Create/edit rules, objects, users (form-heavy UX). |
| `github.com/pressly/goose/v3` | **v3.29.0** | Schema migrations | Embed SQL migrations; version SQLite schema with the binary. |
| `golang.org/x/crypto` | **v0.58.0** | Argon2id password hashing | App-local auth in SQLite (`x/crypto/argon2`). Prefer over bcrypt for new code. |
| `github.com/alexedwards/argon2id` | **v1.0.0** | Thin Argon2id helper | Optional convenience over raw `x/crypto/argon2` (encode/compare helpers). |
| `github.com/stretchr/testify` | **v1.12.1** | Assertions | Unit/integration tests. |
| `github.com/google/go-cmp` | **v0.7.0** | Deep equality | Comparing compiled nft snippets / DTO diffs. |
| `github.com/sqlc-dev/sqlc` | **v1.31.1** (codegen tool) | Typed SQL accessors | Optional but recommended once schema stabilizes — avoid stringly SQL sprawl. |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| GoReleaser **v2.20.0** | Release binaries + packages | Single pipeline: build → checksums → nFPM `.deb`. |
| nFPM **v2.48.0** | Build `.deb` (and later rpm if needed) | Pure Go packager; no Ruby `fpm`. `bindir: /usr/bin`. |
| golangci-lint **v2.15.0** | Lint gate | Pin in CI; enable `govet`, `staticcheck`, `errcheck`, `gosec`. |
| `nft` in CI (Debian container) | Integration tests for compile/check | Run `nft --check` against golden fixtures without loading live rules. |

## Installation

# Module init (example)

# Core runtime deps

# Test

# Host runtime (Debian)

# Release tooling (dev/CI — not linked into the binary)

- Binary → `/usr/bin/nfsense`
- State DB default → `/var/lib/nfsense/nfsense.db` (mode `0600`, owned by service/admin user)
- Generated nft snippets / rollback snapshots → `/var/lib/nfsense/{generated,rollback}/`
- `Depends: nftables`
- Optional systemd unit only if a privileged apply helper is split later; v1 can be root CLI + capability docs
- Ship `lintian-overrides` + `copyright` via nFPM `contents`

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Compile → `nft --check/--file` | `google/nftables` as sole apply path | Incremental netlink edits inside one owned table when you already validated elsewhere; still keep `--check` path for full compile. |
| Compile → `nft` | `sigs.k8s.io/knftables` **v0.0.22** | Learning Check()/Run() patterns / fake Interface for tests. **Do not** make it the product dependency: K8s-scoped, API not stable. |
| Charm Bubble Tea **v2** | Bubble Tea **v1.3.10** (`github.com/charmbracelet/bubbletea`) | Only if a critical Charm v1-only dependency blocks; otherwise greenfield = v2. |
| `modernc.org/sqlite` | `mattn/go-sqlite3` **v1.14.53** | Need bleeding-edge SQLite features / proven cgo perf AND accept cgo in packaging. Default: modernc. |
| Cobra | `urfave/cli/v3` | Tiny single-binary tools; not for deep subcommand trees + hybrid TUI. |
| Argon2id (`x/crypto`) | `bcrypt` (`x/crypto/bcrypt`) | Migrating legacy hashes only. New users: Argon2id. |
| GoReleaser + nFPM | Classic `dh-make` / Debian packaging only | If targeting Debian archive upload with full policy; for project `.deb` releases, nFPM is faster. |
| `database/sql` + goose (+ sqlc) | GORM / ent | Heavy ORMs add little for a config+history DB; prefer explicit SQL. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `mattn/go-sqlite3` as default | cgo → harder cross-compile, fatter CI images, fragile `.deb` builds | `modernc.org/sqlite` |
| `google/nftables`-only apply without `nft --check` | No first-class dry-run of a full candidate ruleset; hard to match Shorewall/VyOS check→commit culture | Compile `.nft` + `nft --check` then apply |
| Ad-hoc `nft add` shell loops | Partial apply leaves host half-configured; SSH lockout risk | Single `nft --file` transaction + armed rollback |
| `nft flush ruleset` in normal apply | Wipes Docker/firewalld/libvirt/CNI tables | Own `table inet nfsense` (or similar) only |
| `sigs.k8s.io/knftables` as core dep | Explicitly K8s-focused; API unstable (`v0.0.x`) | Thin internal `FirewallBackend` wrapping `nft` |
| Charm Bubble Tea **v1** for new code | Module/API moved; greenfield would owe a migration | `charm.land/bubbletea/v2` + companion v2 libs |
| `spf13/viper` by default | Extra config surface when SQLite is SoT | Cobra flags + SQLite; add Viper only if file config appears |
| PHP / privileged web UI | Explicit project Out of Scope; blast radius | CLI + TUI only in v1 |
| iptables / iptables-nft as primary engine | Project bet is nftables | nftables only |
| Ruby `fpm` | Extra Ruby runtime in packaging | nFPM |
| GORM for v1 schema | Overkill; hides SQL; migration story noisier | goose + `database/sql` (+ sqlc later) |
| bcrypt as primary password hash | Weaker/outdated default for new local auth | Argon2id |

## Stack Patterns by Variant

- Cobra + Charm v2 TUI
- SQLite (`modernc`) SoT
- Compile → `nft --check` → arm timed rollback → `nft --file` → verify management path → cancel rollback
- `FirewallBackend` interface so a future `pf` driver is conceivable
- Build binary with modernc (no cgo)
- Test compile/check inside Debian test containers that install `nftables`
- Package `.deb` with `Depends: nftables`
- Add `google/nftables` for listing owned table/chains/sets
- Keep apply path on generated files + `nft` for check semantics
- Stay on v2 for core TUI; vendor a small form widget rather than downgrading whole stack to v1

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| Go **≥1.26** | `golang.org/x/crypto@v0.58.0` | Module declares Go 1.26.0 |
| `charm.land/bubbletea/v2@v2.1.1` | `lipgloss/v2@v2.0.6`, `bubbles/v2@v2.2.1`, `huh/v2@v2.0.3` | Keep Charm stack on matching major (v2 together) |
| `modernc.org/sqlite@v1.62.0` | `database/sql` | Blank-import driver; DSN `file:...?_pragma=foreign_keys(1)` + WAL |
| `google/nftables@v0.3.0` | Linux netlink / nftables kernel | Needs CAP_NET_ADMIN / root to mutate |
| Runtime `nft` | Debian `nftables` package | knftables docs: prefer nft ≥1.0.1; Debian Bookworm+ is fine |
| GoReleaser **v2.20.0** | nFPM **v2.48.0** | Use GoReleaser `nfpms:` integration for `.deb` |

### Prescriptive defaults (do this)

## Confidence by Claim

| Claim | Confidence | Basis |
|-------|------------|-------|
| Module versions above | **HIGH** | Queried `https://proxy.golang.org/<module>/@latest` on 2026-11-08 |
| Charm v2 as greenfield TUI choice | **HIGH** | Context7 Bubble Tea upgrade guide + published `charm.land/*/v2` modules |
| Cobra as CLI standard | **HIGH** | Context7 `/spf13/cobra` + ecosystem prevalence |
| modernc over mattn for this product | **HIGH** | Packaging/static-binary constraints; modernc docs on pkg.go.dev |
| Compile + `nft --check/--file` as primary apply | **HIGH** | nftables operational practice + knftables rationale (CLI is documented API); google/nftables lacks dry-run |
| Avoid knftables as product dependency | **MEDIUM** | Official README: K8s focus, API not stable — pattern OK, dep risky |
| Argon2id over bcrypt | **HIGH** | Current Go crypto guidance / common new-app practice; `x/crypto/argon2` current |
| GoReleaser + nFPM for `.deb` | **HIGH** | Official GoReleaser/nFPM docs |

## Sources

- Context7 `/google/nftables` — Conn batching, `Flush`, `FlushRuleset` (avoid)
- Context7 `/charmbracelet/bubbletea` — v1→v2 upgrade (`charm.land/bubbletea/v2`, declarative `tea.View`)
- Context7 `/spf13/cobra` — `RunE`, persistent flags, command tree
- Context7 `/charmbracelet/lipgloss` — v2 import path `charm.land/lipgloss/v2`
- Context7 / pkg.go.dev `modernc.org/sqlite` — driver registration, DSN/PRAGMA
- pkg.go.dev `golang.org/x/crypto` **v0.58.0** — Go 1.26 requirement
- pkg.go.dev `sigs.k8s.io/knftables` **v0.0.22** — `Check()`/`Run()` pattern; scope warnings
- `proxy.golang.org` — all version pins listed above (fetched 2026-11-08)
- https://nfpm.goreleaser.com/docs/configuration/ — deb packaging
- https://goreleaser.com/customization/package/nfpm/ — GoReleaser ↔ nFPM
- nftables wiki / operational guides — `nft --check`, atomic `--file`, no global flush

<!-- gsd-stack-end -->

<!-- gsd-conventions-start source:CONVENTIONS.md -->

## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- gsd-conventions-end -->

<!-- gsd-architecture-start source:ARCHITECTURE.md -->

## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- gsd-architecture-end -->

<!-- gsd-skills-start source:skills/ -->

## Project Skills

No project skills found. Add skills to any of: `.cursor/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- gsd-skills-end -->

<!-- gsd-workflow-start source:GSD defaults -->

## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:

- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- gsd-workflow-end -->

<!-- gsd-profile-start -->

## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- gsd-profile-end -->
