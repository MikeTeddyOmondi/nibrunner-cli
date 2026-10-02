# Changelog

## Unreleased

- `nibr run <binary>` gained `--depends-on <path-or-url>[=<name>]`, repeatable, for packaging one
  or more additional binaries into the same instance as `<binary>` (one more layer per entry,
  resolved the same way the primary binary is: local path or http(s) URL, destination filename
  defaulting to its own basename or the `=<name>` override). For a primary that's itself a shell
  wrapper execing other programs it depends on. New `internal/deploy.Dependency` (local mode,
  hashes and copies each one into the artifact store the same way the primary is) and
  `internal/remoteapi.Dependency` (`--remote` mode, each uploaded via `UploadArtifact` then sent
  as nibrunner-api's new `dependsOn` field). Verified live against `db9`: redeployed
  `kv-cache-demo` (a real app bundling `locci-kv`, `sqld` and `go-test-server` via a shell
  wrapper) with `--depends-on` twice, confirmed all three layers survived and it's reachable at
  `kv-cache-demo.mt0.dev`.
- `nibr apps list` and `nibr apps status` now show each app's hostnames, in both local and
  `--remote` mode. `reported.json` never carries hostnames of its own, so local mode
  cross-references `desired.json` directly (`internal/cmd.readDesiredHostnames`), and
  `--remote` mode decodes the `hostnames` field `nibrunner-api`'s own `GET /v1/apps`/
  `GET /v1/apps/:app` now return (new `remoteapi.AppView`, wrapping `protocol.ReportedInstance`
  without changing that type itself, since it must stay a faithful mirror of nibrunnerd's wire
  schema). Verified live against `db9`: `hono-demo` (`hono-demo.mt0.dev`) and `opencloud`
  (`opencloud.mt0.dev`, after fixing a real, pre-existing `PROXY_TLS`/`OC_URL` mismatch that only
  surfaced once it had a routable hostname for the first time) both show correctly in `list` and
  `status`, in both modes.
- `nibr run <binary>` now also accepts an http(s) URL in place of a local path (e.g. a GitHub
  release asset), via new `internal/fetch` and `internal/archive` packages. `--archive-member`
  picks the file to deploy out of a `.tar.gz`/`.tgz`/`.zip`; a plain `.gz` unwraps on its own.
  `--sha256` optionally verifies the resolved file before deploying. A local path is unaffected:
  same `os.Stat` check, same error message, byte-for-byte unchanged behavior. Works in both local
  and `--remote` mode, since both only ever receive a resolved local path either way, exactly as
  before this change; the download itself always happens on this machine, including in `--remote`
  mode, not on the nibrunner-api side, so a write-capable network service never makes outbound
  requests to a URL this CLI was merely pointed at. Verified against db9: deployed Caddy directly
  from its GitHub release URL via `nibr run <url> --archive-member caddy --remote ...` and
  confirmed it served real traffic, with the uploaded digest matching an earlier manual
  download-and-upload of the same release byte for byte.
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
