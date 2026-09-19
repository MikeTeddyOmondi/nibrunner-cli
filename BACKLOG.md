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
  supports all three, `nibr` only touches `instances` and `volumes`.
- `internal/remote` and `internal/protocol` have no tests of their own; coverage today is only
  the integration-style tests in `internal/deploy` that exercise them indirectly.

## Ideas, not committed

- A read-only status surface reachable off-box (over the network, not just locally) was the
  original transport idea before landing on "runs locally on the host." Dropped for `nibr` itself,
  but could still be worth revisiting later as a separate concern: nibrunnerd's own metrics
  endpoint is already read-only, so scraping it remotely wouldn't cross the write boundary its
  proxy code defends.
