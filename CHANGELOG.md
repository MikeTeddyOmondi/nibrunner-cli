# Changelog

## Unreleased

- `nibr run <binary> --app <name> --port <n> [...]` deploys a binary: hashes it, copies it
  content-addressed into `/var/lib/nibrunner/artifact-store/`, verifies the copy against the
  digest, then read-modify-writes `/var/lib/nibrunner/desired.json` atomically (temp file +
  rename, same filesystem, so nibrunnerd's watcher never observes a partial document).
- `nibr apps list` / `nibr apps status --app <name>` / `nibr apps logs --app <name> [-n N]` read
  `reported.json` and the per-app log file.
- A redeploy of an existing app keeps its environment and overlays only the keys `--env` names,
  matching nib's own "environment is an edit, not a replacement." Everything else about the
  instance (layer digest, resources, health check) is replaced whole.
- `desired.json`'s `hostId` is read from this host's own `reported.json` and checked against
  before every write; a document naming a different host is refused rather than silently
  overwritten.
- Runs locally, on the same host as `nibrunnerd` (no SSH, no network API). Every write nibrunnerd
  accepts is a local file write by the operator already on the box, which is the boundary
  nibrunnerd's own proxy code is built to trust.
