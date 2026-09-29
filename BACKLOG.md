# Backlog

## Next

- Fold the transfer step nibr itself: today, getting the cross-compiled binary onto `db9` is a
  one-off local HTTP server, done by hand outside `nibr`. A `nibr` subcommand for this should use a
  battle-tested library (`golang.org/x/crypto/ssh` + `github.com/pkg/sftp`), not a hand-rolled
  shell-out (deliberate choice, not an oversight).

## Known gaps

- `apps update` is not implemented yet: only `run` (which redeploys in place), `apps list`,
  `apps status`, `apps logs`, `apps delete`.
- A redeploy never changes an existing volume's `sizeBytes`, even if `--volume-mib` is given a
  different value on the second `run`. Intentional for now (shrinking a live volume is dangerous;
  growing it needs more thought about whether nibrunnerd resizes in place), but undocumented in
  `--help`.
- No flag surface yet for raw ports (`config.ports`), checkpoints, or exports: `desired.json`
  supports all three, `nibr` only touches `instances` and `volumes`. Learned deploying OpenCloud
  (a real multi-service app) that raw ports have a real structural limit worth documenting before
  building the flag: nibrunnerd refuses a `guestPort` that also equals `httpPort` ("guest port
  claimed twice"), and any health check (including `boot-completed`) still probes `httpPort`
  specifically, so an app with only one real listening port cannot use raw ports for it without a
  second, genuinely-listening port to anchor the health check on. Worse: an app that self-verifies
  its own OIDC issuer (or does anything else requiring its external and internal view of its own
  address to match) cannot use raw ports at all, structurally, since nibrunnerd always exposes raw
  ports on a *different* host port than the app's own guest port, and the guest can never reach its
  own external address to begin with. For that shape of app, the working pattern is an SSH tunnel
  straight to `guestIpv4:httpPort` (`ssh -L <port>:<guestIpv4>:<httpPort> <host>`), with `OC_URL`
  (or equivalent) pointed at `127.0.0.1:<port>` so the app's internal and external self-view match
  exactly. Raises the priority of the SSH transport item above: the same battle-tested library
  could also grow a `nibr tunnel` command for this, instead of a hand-typed `ssh -L`.
- `internal/remote` and `internal/protocol` have no tests of their own; coverage today is only
  the integration-style tests in `internal/deploy` that exercise them indirectly.
- As of nibrunner v2026.9.0, per-instance usage metrics (`meters`, `lastHealthyAt`, `compute`,
  per-volume `usage`) moved out of `reported.json` entirely, onto a new metrics/scrape endpoint
  (an OpenAPI-documented filesystem socket, `GET /apps/{appId}/files`, read-only: confirmed no
  write route exists anywhere in the daemon). `nibr apps status` lost its `lastHealthyAt` line as
  a result (removed rather than left dead). Reading that new endpoint to restore equivalent
  status info is a real feature gap, not yet started.

## Ideas, not committed

- A read-only status surface reachable off-box (over the network, not just locally) was the
  original transport idea before landing on "runs locally on the host." Dropped for `nibr` itself,
  but could still be worth revisiting later as a separate concern: nibrunnerd's own metrics
  endpoint is already read-only, so scraping it remotely wouldn't cross the write boundary its
  proxy code defends.
