# Deploying metaframeworks

Framework-specific deploy recipes that build on [CLI_USAGE.md](CLI_USAGE.md)'s primitives (`nibr
run`, `--depends-on`, URL/archive support). Nothing here is special-cased by `nibr` itself: a
metaframework's build output is just one more set of files to package as layers, the same way
[kv-cache-demo's multi-binary example](CLI_USAGE.md) is.

## Next.js

Next's `output: "standalone"` build produces a self-contained Node entrypoint
(`.next/standalone/server.js`) plus a pruned `node_modules`, but it is a directory tree, not a
single binary, and `nibr run` deploys one file per layer. The pattern: package a JS runtime as the
primary binary (`nibr run`'s own URL+`--archive-member` support can fetch it straight from a
GitHub release), and the whole standalone tree as one compressed `--depends-on` dependency that a
shell wrapper unpacks at boot.

Verified end to end against Next.js 16.3.0 (App Router, a server action, and an API route),
running under `bun 1.4.2` inside a nibrunnerd microVM.

### 1. Build the standalone output

```sh
# next.config.ts must set: export default { output: "standalone" } satisfies NextConfig;
npm run build

# .next/static isn't copied into .next/standalone by `next build` itself; do it by hand, same
# for public/ if the app has one.
cp -r .next/static .next/standalone/.next/static
[ -d public ] && cp -r public .next/standalone/public

# Archive the CONTENTS of .next/standalone, not the directory itself, so extracting it later
# lands server.js, node_modules/, .next/, and package.json directly where the wrapper expects.
# COPYFILE_DISABLE=1 on macOS avoids littering the archive with Apple xattr headers that GNU tar
# in the guest will otherwise warn (harmlessly) about on extraction.
COPYFILE_DISABLE=1 tar -czf standalone.tar.gz -C .next/standalone .
```

### 2. Deploy

`nibr run`'s first argument is the *primary* binary; here that's `bun` itself, fetched straight
from its GitHub release and extracted in one step via `--archive-member`. `--depends-on` packages
`standalone.tar.gz` alongside it under its own name. `--program`/`--arg` override what actually
runs: a `/bin/sh -c` wrapper that extracts the archive into a working directory, then execs the
server with `PORT` set to the same port `--port` names.

```sh
nibr run https://github.com/oven-sh/bun/releases/download/bun-v1.4.2/bun-linux-x64.zip \
  --archive-member bun-linux-x64/bun \
  --app next-bun --port 3000 \
  --depends-on ./standalone.tar.gz \
  --program /bin/sh \
  --arg -c --arg 'mkdir -p /app/run && tar -xzf /app/standalone.tar.gz -C /app/run && cd /app/run && PORT=3000 HOSTNAME=0.0.0.0 exec /app/bun server.js' \
  --vcpu 1 --memory-mib 512 \
  --health-kind http --health-path /api/health \
  --hostname next-bun.example.com
```

Works the same way under `--remote`: `nibr` uploads the fetched `bun` binary and
`standalone.tar.gz` to `nibrunner-api` instead of copying them into the local artifact store, same
flags otherwise.

Match the bun asset to the guest, not the build machine: `bun-linux-x64.zip` for a glibc x86_64
guest (check with `ldd --version` inside the guest rootfs, or `cat /etc/os-release`),
`bun-linux-x64-musl.zip` for an Alpine/musl one. If the host's CPU doesn't support AVX2, use the
`-baseline` variant instead (it runs everywhere, just slower). A plain `node` binary works too if
the guest image already has one; skip the archive-member fetch entirely and point `nibr run` at it
directly.

`tar`, not `zip`, for the dependency archive: check what's actually in the guest rootfs before
picking a format. `unzip` is frequently missing from a minimal guest image; `tar`+`gzip` are the
safer default, present on essentially every Linux distribution. If in doubt, deploy a one-off
diagnostic app (any small local binary as a harmless placeholder primary, `--program /bin/sh
--arg -c --arg 'which unzip tar gzip; sleep 600'`) and check `nibr apps logs` before committing to
either.

### Redeploying

A code change means repeating steps 1 and 2 (bun's own `--archive-member` fetch is idempotent and
cheap to repeat, or reuse the already-extracted local `bun` file and drop the URL/archive-member
args). `nibr run` is idempotent per `--app`: running it again under the same app name replaces the
running instance's layers and restarts it under a fresh deployment id, same as any other redeploy.
