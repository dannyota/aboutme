# Release tasks

## Task 6

### Production Caddy image

**Files:**

- Create: `deploy/caddy/production/Caddyfile`
- Create: `deploy/caddy/production/render.sh`
- Create: `deploy/caddy/production/entrypoint.sh`
- Create: `deploy/caddy/production/Dockerfile`
- Create: `deploy/caddy/production/test.sh`
- Modify: `Makefile` (integration owner: add `caddy-prod-test`)

**Interfaces:**

- Produces image `aboutme-caddy`. Environment: `ORIGIN_CERT`, `ORIGIN_KEY`,
  `ORIGIN_PULL_CA` (PEM text), `CLOUDFLARE_RANGES` (space-separated CIDRs).
  Writes the PEM files to `/run/caddy`, which the task mounts as tmpfs.
- Upstreams: Go `127.0.0.1:8080`, Nuxt `127.0.0.1:3000`.

The generated route file `deploy/caddy/public-roots.generated.caddy` targets
Compose hostnames and the peer address. `render.sh` rewrites exactly those
tokens for production and fails on any unexpected count, so a generator change
cannot silently skip the rewrite.

- [x] **Step 1: Write the test**

`deploy/caddy/production/test.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'podman rm -f aboutme-caddy-test >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

routes=$(bash "$root/deploy/caddy/production/render.sh" "$root/deploy/caddy/public-roots.generated.caddy")
grep -q 'reverse_proxy 127.0.0.1:8080' <<<"$routes"
grep -q 'header_up X-Real-IP {client_ip}' <<<"$routes"
if grep -qE 'server:8080|web:3000|remote\.host' <<<"$routes"; then
  echo "render left a Compose token" >&2
  exit 1
fi
printf 'reverse_proxy server:8080 {\n' > "$work/bad.caddy"
if bash "$root/deploy/caddy/production/render.sh" "$work/bad.caddy" >/dev/null 2>&1; then
  echo "render accepted a wrong token count" >&2
  exit 1
fi

podman build -f "$root/deploy/caddy/production/Dockerfile" -t localhost/aboutme/caddy:test "$root"

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 \
  -subj /CN=aboutme.vn -addext subjectAltName=DNS:aboutme.vn,DNS:www.aboutme.vn \
  -keyout "$work/origin.key" -out "$work/origin.pem" 2>/dev/null
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 \
  -subj /CN=pull-ca -keyout "$work/ca.key" -out "$work/ca.pem" 2>/dev/null
openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj /CN=pull \
  -keyout "$work/pull.key" -out "$work/pull.csr" 2>/dev/null
openssl x509 -req -in "$work/pull.csr" -CA "$work/ca.pem" -CAkey "$work/ca.key" \
  -CAcreateserial -days 1 -out "$work/pull.pem" 2>/dev/null

podman run -d --name aboutme-caddy-test -p 127.0.0.1:20451:443 --tmpfs /run/caddy \
  -e ORIGIN_CERT="$(cat "$work/origin.pem")" -e ORIGIN_KEY="$(cat "$work/origin.key")" \
  -e ORIGIN_PULL_CA="$(cat "$work/ca.pem")" -e CLOUDFLARE_RANGES="10.0.0.0/8 192.0.2.0/24" \
  localhost/aboutme/caddy:test >/dev/null
sleep 2

base=(curl -s -o /dev/null -w '%{http_code}' --resolve aboutme.vn:20451:127.0.0.1 --cacert "$work/origin.pem")
if "${base[@]}" https://aboutme.vn:20451/healthz >/dev/null 2>&1; then
  echo "request without the origin-pull certificate succeeded" >&2
  exit 1
fi
code=$("${base[@]}" --cert "$work/pull.pem" --key "$work/pull.key" https://aboutme.vn:20451/healthz || true)
[[ "$code" == "502" ]] || { echo "want 502 with no upstream, got $code" >&2; exit 1; }
podman exec aboutme-caddy-test sh -c 'test ! -r /proc/1/environ || ! grep -qa ORIGIN_KEY= /proc/1/environ'
echo "caddy-prod-test: ok"
```

- [x] **Step 2: Run it and confirm it fails**

```sh
bash deploy/caddy/production/test.sh
```

Expected: fails because `render.sh` does not exist.

- [x] **Step 3: Write `render.sh`**

```bash
#!/usr/bin/env bash
# Rewrites the generated Compose route table for the production host.
set -euo pipefail
input=$1
content=$(cat "$input")
replace() {
  local from=$1 to=$2 want=$3 got
  got=$(grep -oF -- "$from" <<<"$content" | wc -l)
  if [[ "$got" -ne "$want" ]]; then
    echo "render: expected $want of '$from', found $got" >&2
    exit 1
  fi
  content=${content//"$from"/"$to"}
}
replace 'reverse_proxy server:8080' 'reverse_proxy 127.0.0.1:8080' 2
replace 'reverse_proxy web:3000' 'reverse_proxy 127.0.0.1:3000' 1
replace '{http.request.remote.host}' '{client_ip}' 2
printf '%s\n' "$content"
```

- [x] **Step 4: Write the Caddyfile**

```caddyfile
{
    admin off
    servers {
        trusted_proxies static {$CLOUDFLARE_RANGES}
        trusted_proxies_strict
        client_ip_headers CF-Connecting-IP
    }
}

(origin_tls) {
    tls /run/caddy/origin.pem /run/caddy/origin-key.pem {
        client_auth {
            mode require_and_verify
            trust_pool file /run/caddy/origin-pull-ca.pem
        }
    }
}

www.aboutme.vn {
    import origin_tls
    redir https://aboutme.vn{uri} permanent
}

aboutme.vn {
    import origin_tls
    encode zstd gzip

    request_header -X-Real-IP
    request_header -X-Forwarded-For
    request_header -Forwarded

    import /etc/caddy/routes.caddy

    handle {
        reverse_proxy 127.0.0.1:3000
    }
}
```

`{client_ip}` is the `CF-Connecting-IP` value only when the peer is inside
`CLOUDFLARE_RANGES`; otherwise it is the peer address.

- [x] **Step 5: Write the entrypoint and Dockerfile**

`entrypoint.sh`:

```sh
#!/bin/sh
set -eu
umask 077
: "${ORIGIN_CERT:?}" "${ORIGIN_KEY:?}" "${ORIGIN_PULL_CA:?}" "${CLOUDFLARE_RANGES:?}"
printf '%s\n' "$ORIGIN_CERT" > /run/caddy/origin.pem
printf '%s\n' "$ORIGIN_KEY" > /run/caddy/origin-key.pem
printf '%s\n' "$ORIGIN_PULL_CA" > /run/caddy/origin-pull-ca.pem
unset ORIGIN_KEY
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
```

`Dockerfile` (pin the base image by the digest of `caddy:2.11.4-alpine` found
with `podman pull` and `podman image inspect --format '{{.Digest}}'`):

```dockerfile
FROM docker.io/library/alpine:3.24 AS render
RUN apk add --no-cache bash grep
COPY deploy/caddy/production/render.sh /render.sh
COPY deploy/caddy/public-roots.generated.caddy /generated.caddy
RUN bash /render.sh /generated.caddy > /routes.caddy

FROM docker.io/library/caddy:2.11.4-alpine@sha256:<pinned digest>
COPY deploy/caddy/production/Caddyfile /etc/caddy/Caddyfile
COPY --from=render /routes.caddy /etc/caddy/routes.caddy
COPY --chmod=0755 deploy/caddy/production/entrypoint.sh /usr/local/bin/aboutme-caddy
ENTRYPOINT ["/usr/local/bin/aboutme-caddy"]
```

Replace `<pinned digest>` with the real digest before running Step 6.

- [x] **Step 6: Run the test**

```sh
bash deploy/caddy/production/test.sh
```

Expected: `caddy-prod-test: ok`. Ask the integration owner to add
`caddy-prod-test: ; bash deploy/caddy/production/test.sh` to the `Makefile`.

Result (2026-09-16): `make caddy-prod-test` passes. The implemented test also
runs a stand-in for Go inside the Caddy network namespace and proves that Go
receives the peer address from an untrusted peer and `CF-Connecting-IP` from a
trusted one. Caddy's `permanent` redirect returns 301, not 308. Removing the
client certificate requirement or the `CF-Connecting-IP` setting makes the test
fail.

## Task 7

### Tag workflow builds, smokes and publishes images

**Files:**

- Create: `.github/workflows/release-images.yml`

**Interfaces:**

- Consumes: `deploy/server.Dockerfile` (Task 5), `deploy/web.Dockerfile`,
  `deploy/caddy/production/Dockerfile` (Task 6).
- Produces: public packages `ghcr.io/dannyota/aboutme-server`, `aboutme-web` and
  `aboutme-caddy`, tagged with the Git tag, plus a `digests.txt` artifact with
  lines `server=sha256:...`, `web=sha256:...`, `caddy=sha256:...`. Task 13 reads
  digests from the registry, not the artifact.

The workflow holds no cloud credential and never deploys.

- [x] **Step 1: Resolve action pins**

```sh
gh api repos/actions/attest-build-provenance/releases/latest --jq .tag_name
gh api repos/actions/attest-build-provenance/git/ref/tags/<tag> --jq .object.sha
```

Use `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1` as in
`ci.yml`, and the attestation SHA from above.

- [x] **Step 2: Write the workflow**

```yaml
name: release-images

on:
  push:
    tags: ["v*"]

permissions:
  contents: read

jobs:
  image:
    runs-on: ubuntu-24.04-arm
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
    strategy:
      fail-fast: true
      matrix:
        include:
          - name: server
            dockerfile: deploy/server.Dockerfile
            smoke:
              docker run --rm --entrypoint sh "$IMAGE" -c 'test -s
              /etc/ssl/rds/global-bundle.pem && /opt/chromium/chrome --version
              && test -x /usr/local/bin/db-setup'
          - name: web
            dockerfile: deploy/web.Dockerfile
            smoke:
              docker run -d --name smoke -p 127.0.0.1:3000:3000 "$IMAGE" &&
              sleep 5 && curl -fsS -o /dev/null http://127.0.0.1:3000/
          - name: caddy
            dockerfile: deploy/caddy/production/Dockerfile
            smoke:
              docker run --rm --entrypoint caddy -e
              CLOUDFLARE_RANGES=192.0.2.0/24 "$IMAGE" adapt --config
              /etc/caddy/Caddyfile >/dev/null
    env:
      IMAGE: ghcr.io/dannyota/aboutme-${{ matrix.name }}:${{ github.ref_name }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - run:
          docker build --platform linux/arm64 -f ${{ matrix.dockerfile }} -t
          "$IMAGE" .
      - run: ${{ matrix.smoke }}
      - run:
          echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u "${{
          github.actor }}" --password-stdin
      - id: push
        run: |
          docker push "$IMAGE"
          digest=$(docker inspect --format '{{index .RepoDigests 0}}' "$IMAGE" | cut -d@ -f2)
          echo "digest=$digest" >> "$GITHUB_OUTPUT"
          echo "${{ matrix.name }}=$digest" > digest-${{ matrix.name }}.txt
      - uses: actions/attest-build-provenance@<sha from step 1> # <tag from step 1>
        with:
          subject-name: ghcr.io/dannyota/aboutme-${{ matrix.name }}
          subject-digest: ${{ steps.push.outputs.digest }}
          push-to-registry: true
      - uses: actions/upload-artifact@<pin resolved the same way> # <tag>
        with:
          name: digest-${{ matrix.name }}
          path: digest-${{ matrix.name }}.txt
```

Replace each `<...>` pin with the resolved SHA and tag before committing.

- [x] **Step 3: Check the workflow locally**

```sh
npx prettier --check .github/workflows/release-images.yml
```

Expected: clean. A real run needs a tag; Task 14 creates the first one.

- [ ] **Step 4: Make the packages public**

After the first run, the owner sets each of the three GHCR packages to public in
the GitHub package settings, so the host pulls without a credential.

Result (2026-09-16): the workflow is written; `actionlint` and Prettier pass.
Each image uploads its own `digest-<name>` artifact and writes its digest to the
job summary. Writing it found that the server image pinned the amd64-only
Playwright manifest and a Chromium path that exists only on amd64; commit
`e68c37b` pins the multi-architecture index and picks `chrome-linux` on arm64.
The first real run happens at Task 14's tag; Step 4 waits for it.
