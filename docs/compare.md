# nfsense compared with similar tools

nfsense is **pre-alpha**: there is no release, no binary, and no installer. This document compares *intent and design*, not shipped behavior. Treat every "nfsense" cell as a claim about what the design commits to, and every competitor cell as verified fact from the linked repository or man page.

The point of writing this down is honesty: most of what a firewall manager does is table stakes, and claiming otherwise would be the fastest way to lose a knowledgeable reader.

## TL;DR

- The **config → compile → `nft -f`** pipeline is not a differentiator. `openwrt/firewall4` and `foomuuri` already do it, and they do it well.
- **Zones** are not in nfsense v1. The competitor closest to us ships them today. This is the weakest part of our positioning and should not be dressed up.
- The one thing nfsense adds that none of these ship is **transactional apply with a timed confirm, automatic soft rollback, and a persistent record of what was applied, when, and by whom** — as a first-class primitive with state in SQLite, not a shell wrapper around `nft`.
- That pattern is **not new**. `iptables-apply` has shipped in Debian since the mid-2000s. What is missing is an nftables-native implementation with durable state, an owned-table model, and a real config source.

## nfsense is / is not

| nfsense **is** | nfsense **is not** |
|---|---|
| A single-host Debian manager for `nftables`, with intended state in SQLite | A router distribution or appliance firmware |
| A draft → check → apply → confirm → rollback pipeline where every step is explicit | A daemon that continuously reconciles a ruleset |
| A tool that writes only its own `table inet nfsense` | A `flush ruleset` hammer that owns the whole kernel |
| A scriptable CLI now, a TUI later, over one domain layer | Two implementations that drift apart |
| Local app auth in SQLite: who may change policy | Network identity, SSO, or a control plane |
| A Linux tool | A FreeBSD/`pf` tool (the seam exists; the driver does not) |

## Snapshot

| | **nfsense** | [firewall4](https://github.com/openwrt/firewall4) | [foomuuri](https://github.com/FoobarOy/foomuuri) | [csf-firewall](https://github.com/Aetherinox/csf-firewall) | `nft` + your own script |
|--|------------|------------|------------|------------|------------|
| Ships today | **No — pre-alpha** | Yes (OpenWrt) | Yes, v0.x | Yes, since 2005 | Yes |
| Runtime | Go, single static binary | ucode + shell (OpenWrt mirror) | Python | Perl + shell | `nft` |
| Config source | SQLite (intended state) | UCI text files | Text zone/rule language | Text config + ipset lists | Handwritten ruleset file |
| Apply model | compile → `nft --check` → atomic load | generate → `nft -f` | generate → load | writes iptables/nftables rules | `nft -f` |
| Timed confirm + auto-rollback | **Yes — Core Value** | No in the config model (OpenWrt's "apply unchecked" timeout lives in LuCI/ubus above it) | No | No | No, unless you build it |
| Persistent apply history (who/when/result) | **Yes — SQLite, append-only** | No | No | Partial (log lines) | No |
| Zones | **No (v1.x)** | Yes | Yes | No | Whatever you write |
| NAT / SNAT / DNAT | Yes (Phase 5) | Yes | Yes | Yes | Whatever you write |
| Owns only its own tables | Yes | Yes (`table inet fw4`) | Yes | Mixed | Depends entirely on you |
| Debian-native, no framework | Yes | No — OpenWrt only | Yes | Yes | Yes |
| Local policy-change auth | Yes (Phase 6) | No | No | Partial (panel users) | No |
| TUI | Planned (Phase 7) | No | No | No | No |
| License | MIT | — | GPL-2.0 | Other | — |
| GitHub stars (2026-11-08) | 0 — unreleased | 27 (mirror of `git.openwrt.org`) | 119 | 382 | n/a |

## What each one answers

| Question | **nfsense** | firewall4 | foomuuri | csf | `nft` + script |
|----------|------------|-----------|----------|-----|----------------|
| Where does the intended policy live? | SQLite, revisioned | UCI | Text files | Text config | The file you edited |
| Can I see what changed before it hits the kernel? | `nfsense diff` (Phase 2) | `fw4 print` | Generated ruleset | Partial | `nft --check` |
| What happens if the change locks me out? | Timed confirm; auto-rollback to the last good revision | You reconnect or you don't. LuCI can revert if you kept the tab open | Manual | Manual | You hope |
| Can I prove who applied it? | `actor` on every applied revision | No | No | Log line | No |
| Does it touch other tools' tables? | Never | No | No | Mixed | Depends on your script |
| Does it survive reboot with the last good state? | Boot restore (Phase 3) | Yes, from UCI | Yes, from its config | Yes | Only if you wire a unit |
| Install on a plain Debian server? | Yes (target) | No | Yes | Yes | Yes |

## Where nfsense is genuinely different

One thing, and it is narrow:

**Apply is a transaction with a timeout, and the outcome is recorded.** Snapshot the prior owned state, arm a confirm, load atomically, and if nobody confirms within the window, restore. Every applied revision is a row with `actor`, `status`, rendered `artifact` and its hash.

That is what a `nft` script plus a systemd timer cannot give you easily: durable state, a rollback source that is *the artifact that was actually loaded* (not a re-compilation under a newer version), and an audit trail that answers "who changed the policy on Tuesday".

The rest — zones, NAT, objects, logging, filtering — is table stakes, and other projects do some of it better than we will at v1.

## Where nfsense is weaker today

| Weakness | Reality |
|---|---|
| **No zones in v1** | `PROJECT.md` cites Shorewall's zone-oriented thinking as an inspiration, then defers zones to v1.x. `foomuuri` and `firewall4` both ship zones now. This is an honest gap, not a roadmap flourish. |
| Nothing runs yet | Every nfsense claim above is a design commitment. Until Phase 1 lands a binary, the column is intent. |
| No router story | Single host, local nftables only. `firewall4` is a better answer if you are running OpenWrt. |
| No dynamic lists, D-Bus, port knocking, geolocation | `foomuuri` has all of it. |
| No web UI | Deliberate, but it means no dashboard, no charts, no remote console. |

## When to pick something else

- **You run OpenWrt** → `firewall4`. It is the native answer, and this project does not run on your platform.
- **You want zones today, on Debian, with a rich rule language** → `foomuuri`. It is the most direct alternative and it is more capable than nfsense will be for several phases.
- **You want a hosting-panel security suite** (login monitoring, blocklists, exploit rules) → `csf-firewall`. That is a different product with a different scope.
- **You want to embed nftables control in a Go program** → [`google/nftables`](https://github.com/google/nftables). It is a library, not a manager; its own README calls it early-stage. nfsense deliberately does *not* depend on it for the apply path, because it offers no `nft --check` equivalent.
- **You want the confirm-and-rollback safety net and nothing else, today, on iptables** → `iptables-apply` from the `iptables` package.

## Prior art worth knowing

[`iptables-apply(8)`](https://manpages.debian.org/bookworm/iptables/iptables-apply.8.en.html) is described as *"a safer way to update iptables remotely"*: it applies a ruleset, prompts for confirmation, and rolls back after a timeout if the operator cannot answer. It has shipped in Debian since the mid-2000s, with `ip6tables-apply` alongside it.

That is the same safety idea nfsense builds on. We are not claiming the concept. We are claiming that it has no nftables-native implementation with durable state, an owned-table model, an artifact-based rollback source, and an audit trail — and that this is the piece worth building carefully.

## Sources

- `openwrt/firewall4` — repository metadata and language mix, 2026-11-08
- `FoobarOy/foomuuri` — README feature list, license, stars, 2026-11-08
- `Aetherinox/csf-firewall` — README summary ("front-end to iptables or nftables", started 2005), 2026-11-08
- `google/nftables` — README ("very early stages… only enough to install very basic nftables rules"), 2026-11-08
- `iptables-apply(8)` — Debian manpages, iptables 1.8.9
- nfsense roadmap and requirements — `.planning/ROADMAP.md`, `.planning/REQUIREMENTS.md` (not published)
