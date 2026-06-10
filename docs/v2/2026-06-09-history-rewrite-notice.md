# Git history rewrite — 2026-06-09

The grimnir_radio repo's git history was rewritten on 2026-06-09 to scrub internal IP addresses, SSH usernames, and an internal hostname that had been committed across docs, configs, tests, & design plans. The substrate was sound; the leak was operational hygiene.

## What was scrubbed

Every commit reachable from any branch or tag now reads the placeholders below; the real values appear in no public history. Operators substitute real values from their private topology notes.

| Placeholder | Was |
|---|---|
| `<node-a-ip>` | proxmox VM A IP |
| `<node-b-ip>` | proxmox VM B IP |
| `<listener-vip>` | keepalived listener VIP |
| `<dj-vip>` | keepalived DJ VIP |
| `<redis-host>` | Redis primary IP |
| `<control-plane-host>` | reserved control-plane IP |
| `<v1-prod-host>` | the current v1 prod host's IP |
| `<edge-vps>` | edge VPS jump host IP |
| `<lab-host-c>`, `<lab-host-d>` | lab test peer IPs in plan docs |
| `<example-db-host>` | example DB host from v1 migration docs |
| `<ssh-user>` | SSH username on prod |
| `<v1-hostname>` | v1 prod host's hostname |
| `<public-hostname>` | public domain |

Phase 1 also sanitized the working tree (commits `f3efeda`, `50cfb23`, `35e20d8`, `3db404e`), added `ops/operator-topology.template.yml` for the operator to fill in privately, and gitignored the `*.local.yml` sibling.

## Resync (REQUIRED for anyone with a pre-rewrite clone)

Every commit SHA changed. A `git pull` against an old clone refuses with a non-fast-forward error. The fix:

```bash
cd <repo-root>
git fetch
git reset --hard origin/<branch>      # main or v2-dev or whichever you're on
```

For the prod server (which carries its own clone for `./grimnir pull`):

```bash
ssh <ssh-user>@<v1-prod-host>
cd /srv/docker/grimnir_radio
git fetch
git reset --hard origin/main
```

After the reset, `./grimnir pull` & `./grimnir up -d` resume working normally.

## Tags

All 470 tags were rewritten in place; tag names are unchanged but the commits they point at have new SHAs. Anyone with a tag-based deploy pipeline gets the new SHAs automatically on next `git fetch --tags --force`.

## What did NOT change

- File contents, file paths (one rename: `rlmradio.xyz-ai.md` → `docs/station-blueprint-ai.md`), or commit messages other than substitution within
- The codebase's behavior; `make ci` exit 0 verified at the rewrite point
- Per-binary feature set; the v2.0.0-rc.10 tag points at the rewritten equivalent of rc.9 + sanitization commits
- The operator's authoritative real-value mapping; keep that private (password manager, gitignored local file, or a separate private repo)

## Why now

Defense-in-depth. The leaked IPs were RFC1918 (not directly routable from the internet) but published topology + valid SSH username on a public repo hands attackers reconnaissance for free if they ever get a foothold.

## Why not just delete the public repo & start over

Tag history exists as an artifact of every release cut on the v1 line & every alpha/rc cut on the v2 line. Operators tracking from those tags would lose the audit trail. A clean rewrite preserves the trail while removing the sensitive strings.
