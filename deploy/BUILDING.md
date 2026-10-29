# Local builds and compact runtime images

Source compilation belongs on the development machine (Windows WSL for the
Ritel deployment). Production only receives verified artifacts. No script in
this document accesses a production host or changes Compose.

## Build an embedded artifact

Install the exact `packageManager` from `frontend/package.json` and the Go
toolchain specified by `backend/go.mod`. Prepare dependencies with
`pnpm --dir frontend install --frozen-lockfile` and `go mod download` from
`backend/`. Neither the release builder nor image packaging runs `go mod tidy`
or changes dependency versions.

Commit the intended source, then run:

```sh
python3 deploy/build-local.py --ritel \
  --go /home/ritel/.local/lib/go1.27.3/bin/go \
  --pnpm /path/to/pnpm-9.15.9 \
  --output /home/ritel/.cache/sub2api-build/RELEASE
```

`--ritel` sets only the public `https://account.ritel.si` and
`https://dash.ritel.si` build origins. Generic upstream builds omit `--ritel`;
other installations can pass both `--account-origin` and `--console-origin`.
The builder rejects active frontend `.env` files and ignores inherited `VITE_`
variables, keeping release configuration explicit. No `.env` file is copied
into an artifact or packaging context.

The builder first builds the frontend into `frontend/dist`, compares every
file while copying it into `backend/internal/web/dist`, tests the embedded
server, then compiles `CGO_ENABLED=0`, `linux/amd64`, `-tags embed`, `-trimpath`,
and `-mod=readonly`. Use `--arch arm64` when needed. It records the full commit,
tree, public origins, exact tool versions, frontend/resource hashes, module and
lockfile hashes, and binary SHA in `manifest.json`. The default build date is
the commit timestamp; `SOURCE_DATE_EPOCH` can override it. Repeated builds with
the same source, toolchains, inputs, origins and timestamp can be compared by
binary SHA. `--offline` requires cached Go modules. Build output must be a new
directory outside the source tree; failures keep their logs for inspection.

`--plan` prints the commands without building. `make build-ritel` calls this
builder (`BUILD_ARGS` supplies options). The generic `make build` also builds
the frontend before compiling its embedded backend, including under `make -j`.
`make build-backend` remains the lightweight non-embedded development build.

## Package on a fixed runtime

Use an independently verified runtime-only image, addressed by immutable image
ID or repository digest. It must contain the required OS libraries, CA roots,
timezone database, UID/GID 1000, entrypoint, healthcheck, and version-compatible
`pg_dump`/`psql`, and contain no previous application binary in any layer. For
an existing deployment, verify its full image Config and filesystem contract
when creating this baseline. A new Alpine build with mutable package indexes
does not establish equivalence with the deployed runtime.

```sh
python3 deploy/package-runtime.py \
  --manifest /home/ritel/.cache/sub2api-build/RELEASE/manifest.json \
  --runtime-image sha256:APPROVED_RUNTIME_IMAGE_ID \
  --tag sub2api:RELEASE
```

The packaging context contains only the verified binary, fallback resources,
Dockerfile and ignore file. It uses no network or compiler, inherits the exact
runtime Config, and adds two layers (binary and small resources). Ownership is
set by `COPY --chown`, so no later recursive `chown` creates another binary
copy. The packager verifies immutable base identity, backup tools, platform,
image Config, layer shape, executable ownership/mode and packaged binary SHA.
It writes an image receipt beside the artifact. Creation and approval of the
runtime-only baseline are separate gates: absence of a visible old binary
does not prove that historical layers are clean.

The root `Dockerfile` remains the generic complete source build;
`Dockerfile.goreleaser` remains the upstream prebuilt-binary packaging path.
For reproducible container source builds, pass immutable Node/Go/Alpine/Postgres
image digests and explicit `VERSION`, `COMMIT` and `DATE` arguments. Both source
Dockerfiles use a fixed frontend output path, accept the public origin build
arguments, and retain PostgreSQL backup clients. Their `.dockerignore` excludes
old generated embeds and operational/private material.

## Release and recovery

A compact new image does not itself free disk while old images or backup
manifests still reference the old layers. Keep a verified compact rollback
image and existing recovery references, update the release and archive
allowlists through their normal locks/CAS workflow, then retire only an exact
approved set after independent verification. Do not use a global prune,
delete containerd content directly, or overwrite production business data.
