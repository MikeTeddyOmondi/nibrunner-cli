# Changelog

## Unreleased

- Added an opt-in `--remote <url>` / `--remote-token <token>` (or `$NIBR_REMOTE_URL` /
  `$NIBR_REMOTE_TOKEN`) mode, backed by the new `internal/remoteapi` package. With it set, `run`,
  `apps list/status/logs/delete`, and `host status` talk to a `nibrunner-api` instance (a
  separate, sibling repo: a Gin HTTP wrapper that performs the same file I/O this CLI does
  locally, co-located with the target nibrunnerd) over HTTP instead of this host's own files.
  Unset, which remains the default, nothing changes: every command still operates on local files
  exactly as before. Verified against a real nibrunnerd on the `db9` testbed: deployed a real
  release binary (Caddy) from a separate machine over Tailscale, watched it converge, read its
  logs, and deleted it, with results matching the equivalent local commands run on the host
  itself.
- Updated for nibrunner v2026.9.0 (the fork's first tagged upstream release), which added a
  required top-level `revision` field to `desired.json` (a control-plane version string;
  nibrunnerd refuses a document without one outright). `writeDesired` now bumps it fresh on every
  write, the one place every write goes through, so no call site can forget it. `reported.json`
  gained matching `acceptedDigest`/`acceptedRevision` fields, now surfaced through a new `nibr host
  status` command. Also removed `lastHealthyAt` from `ReportedInstance` and `nibr apps status`:
  nibrunnerd no longer writes it (moved to a new metrics endpoint outside `reported.json`).
- Real-world validation: used `--program`/`--arg` to deploy OpenCloud (a full multi-service Go
  app, not a toy binary) via `nibr run`, with an idempotent init-then-serve shell wrapper. Surfaced
  and fixed six separate instances of the same upstream bug class in OpenCloud itself (several
  internal service URLs default to the literal hostname `localhost`, which this guest image cannot
  resolve), and confirmed the raw-ports/OIDC structural limit now written up in BACKLOG.md.
- `nibr run` gains `--program` (run something other than the deployed binary, e.g. `/bin/sh`, with
  the binary still deployed and reachable under `--working-dir`) and repeatable `--arg` (one exact
  argument per occurrence, unlike `--args`'s naive space-split, for anything with embedded spaces
  or quoting: a shell wrapper doing idempotent first-boot setup before exec'ing the real program).
- Fixed: `nibr apps delete` marks an app's volume absent; redeploying under the same appId used
  to leave it absent forever, since `nibr run`'s volume upsert only checked whether the volumeId
  already existed, not its desiredState, so nibrunnerd kept refusing to serve the new instance
  ("this host does not serve <volumeId>"). Caught live on db9 and hand-patched; a redeploy now
  revives an absent volume back to present.
- Fixed: `nibr run` with no `--env` at all used to write `"environment": null` into desired.json
  on a fresh deploy (a nil Go map marshals to JSON null). nibrunnerd's Rust side requires an
  actual map there, so a null failed the whole document's parse and stopped reconciliation for
  every app on the host, not just the new one. Caught live on db9 and hand-patched; `nibr` now
  always writes `"environment": {}` when none is given.
- `nibr run <binary> --app <name> --port <n> [...]` deploys a binary: hashes it, copies it
  content-addressed into `/var/lib/nibrunner/artifact-store/`, verifies the copy against the
  digest, then read-modify-writes `/var/lib/nibrunner/desired.json` atomically (temp file +
  rename, same filesystem, so nibrunnerd's watcher never observes a partial document).
- `nibr apps list` / `nibr apps status --app <name>` / `nibr apps logs --app <name> [-n N]` read
  `reported.json` and the per-app log file. `apps logs` on an app that has not booted yet (`nibr
  run` returns as soon as desired.json is written, before nibrunnerd's next reconcile pass boots
  the instance) says so plainly, instead of a bare "file does not exist".
- `nibr apps delete --app <name> [--keep-volume]` removes an app's instance from desired.json, so
  nibrunnerd tears its microVM down on its next reconcile pass, and marks its volume absent unless
  `--keep-volume` is given.
- Cross-compiled (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`) and run for real on `db9`: `nibr run`
  deployed a live app against the fixed `nibrunnerd` build, converged to healthy, and `apps
  list`/`status`/`logs` all read it back correctly.
- A redeploy of an existing app keeps its environment and overlays only the keys `--env` names,
  matching nib's own "environment is an edit, not a replacement." Everything else about the
  instance (layer digest, resources, health check) is replaced whole.
- `desired.json`'s `hostId` is read from this host's own `reported.json` and checked against
  before every write; a document naming a different host is refused rather than silently
  overwritten.
- Runs locally, on the same host as `nibrunnerd` (no SSH, no network API). Every write nibrunnerd
  accepts is a local file write by the operator already on the box, which is the boundary
  nibrunnerd's own proxy code is built to trust.
