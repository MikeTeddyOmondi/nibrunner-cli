# CLI usage

`nibr` writes `/var/lib/nibrunner/desired.json` the only way nibrunnerd's own proxy code
trusts: a local, atomic, whole-document file write by the operator already on the box. That's
the default for every command below. `--remote` is the one opt-in exception, see
[Remote mode](#remote-mode).

## `nibr run <binary>`

Deploy a binary as an app on this host.

```sh
nibr run ./my-server --app my-app --port 8080
```

Required: `--app` (the app's identifier) and `--port` (the guest port the binary listens on).

```sh
nibr run ./my-server \
  --app my-app \
  --port 8080 \
  --env LOG_LEVEL=debug \
  --env API_KEY=secret \
  --vcpu 1 \
  --memory-mib 256 \
  --volume-mib 512 \
  --health-kind tcp
```

Redeploying the same `--app` upserts it: a fresh `deploymentId` is generated every run, and
`--env` is merged into the app's existing environment rather than replacing it, so secrets set
once don't need retyping on every redeploy.

Passing arguments to the binary:

```sh
# space-split, for simple cases
nibr run ./my-server --app my-app --port 8080 --args "serve --verbose"

# repeatable, one exact argument each, for anything --args would mangle
# (quoting, embedded spaces, a shell wrapper)
nibr run ./my-server --app my-app --port 8080 \
  --program /bin/sh \
  --arg -c --arg "test -f /app/data/ready || /app/my-server init; exec /app/my-server serve"
```

`--program` overrides what's actually executed (e.g. `/bin/sh`), while the deployed binary
still lands at `--working-dir`/`<binary basename>` for that program to exec.

Other flags: `--working-dir` (default `/app`), `--data-dir-flag` (appended with
`<working-dir>/data` if set), `--health-path` (required if `--health-kind http`), `--hostname`
(route the proxy to this app on a hostname).

## `nibr apps list`

```sh
nibr apps list
```

```
APP                  STATE        RESTARTS   MESSAGE
my-app               running      0
```

## `nibr apps status --app <name>`

```sh
nibr apps status --app my-app
```

```
  appId:         my-app
  deploymentId:  my-app-1790855985780711251
  state:         running
  hostPort:      21016
  guestIpv4:     10.201.0.10
  restartCount:  0
  startedAt:     2026-10-01T11:54:51.871Z
```

## `nibr apps logs --app <name>`

```sh
nibr apps logs --app my-app
nibr apps logs --app my-app -n 50   # last 50 lines (default 100)
```

## `nibr apps delete --app <name>`

```sh
nibr apps delete --app my-app
```

Also marks the app's volume absent, so nibrunnerd reclaims it. Keep it instead (for a later
redeploy under the same app name to reuse it):

```sh
nibr apps delete --app my-app --keep-volume
```

## `nibr host status`

```sh
nibr host status
```

```
  hostId:        host-local
  state:         ready
  reportedAt:    2026-10-01T11:59:25.444Z
  acceptedDigest: 00fb80464eb91c3a984eefb58a7d1ca3096dc0e93e06c53afd0df9aced0fd26a
  acceptedRevision: nibrunner-api-1790855724257363623
  capacity:      2 vCPU, 3214 MiB
  allocatable:   0 vCPU, 654 MiB
```

## Remote mode

By default `nibr` only ever touches this host's own local files. Set `--remote` (or
`$NIBR_REMOTE_URL`) to point every command above at a
[nibrunner-api](https://github.com/MikeTeddyOmondi/nibrunner-api) instance instead, so they can
run from a different machine over HTTP. `nibrunner-api` must be running co-located with the
target nibrunnerd; it performs the same file I/O `nibr` does locally, just reachable off-box.

```sh
export NIBR_REMOTE_URL="http://db9:8282"
export NIBR_REMOTE_TOKEN="your-token-here"

nibr host status
nibr apps list
nibr run ./my-server --app my-app --port 8080
nibr apps status --app my-app
nibr apps logs --app my-app
nibr apps delete --app my-app
```

Equivalent, one-off, without exporting env vars:

```sh
nibr apps list --remote http://db9:8282 --remote-token your-token-here
```

Every command's output is identical in shape between local and remote mode: `nibr apps status
--app my-app --remote http://db9:8282` and `nibr apps status --app my-app` (run directly on the
host) print the same fields, since both are just readers of the same `reported.json`.

`--remote-token` (or `$NIBR_REMOTE_TOKEN`) is unnecessary (and can be omitted) only if the
target `nibrunner-api` is running with `AUTH_ENABLE=false`.
