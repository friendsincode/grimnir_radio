# Grimnir Radio

Version: 0.0.1-alpha (Documentation Alpha)

Grimnir Radio is a Go-based radio suite inspired by the vision of building a modern, reliable control plane for scheduling, audio analysis, and playout targeting Icecast/Shoutcast without Liquidsoap.

## Naming

- Canonical name: Grimnir Radio
- Go module: `github.com/example/grimnirradio`
- Binary path: `cmd/grimnirradio`
- Env vars: prefer `GRIMNIR_*` (falls back to `RLM_*` for compatibility)

## Docs

- Sales spec: `docs/specs/SALES_SPEC.md`
- Engineering spec: `docs/specs/ENGINEERING_SPEC.md`
- Programmer's spec: `docs/specs/PROGRAMMERS_SPEC.md`
- Archived specs: `docs/olderspecs/`
- First-time setup (Ubuntu/Debian + VS Code): `docs/SETUP_VSCODE.md`

## Changelog

- See `docs/CHANGELOG.md` for version history.

## Development

- Verify code: `make verify` (tidy, fmt, vet, optional lint, test)
- Build binary: `make build` (outputs `./grimnirradio`)

## Shout-Outs

Special shout-out to Sound Minds, Hal, Vince, MooseGirl, doc mike, Grammy Mary, Flash Somebody, Cirickle, and everyone else trying to keep RLM alive.

Grimnir, may your dream live on in this project.
