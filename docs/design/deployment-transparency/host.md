# Deployment document on the Vietnam host

The GreenNode vServer reports its running Podman containers at
`/.well-known/deployment.json`. The reporter runs on the host, outside the
application containers. It is not an independent platform API. Its report is
weaker evidence than the AWS ECS observer's report
([ADR 0028](../../adr/0028-deployment-transparency-observer.md)).

## Source and publication

`deploy/vn/host/deployment-document.sh` inspects the fixed containers
`aboutme-server`, `aboutme-web`, `aboutme-caddy`, and `aboutme-maintenance`.
Only containers whose Podman state is running count. The reporter takes the
manifest digest from `ImageDigest`, not the image configuration ID, an image
tag, container labels, or `release.json`. It normalizes `State.StartedAt` to
whole UTC seconds and records one replica per running container.

A systemd oneshot runs every minute and shortly after boot. A failed Podman
read, malformed digest, or malformed timestamp stops publication. Missing or
stopped required containers have empty image lists and produce `mismatch`.
Maintenance appears only while running. The previous document remains on a
failed read and ages into stale under the existing freshness rules.

The reporter builds the [document](document.md) from constants and the four
allowed observation fields. It never reads container environment values or
secret files. It uses formatted Podman output, discards command error output,
and logs only fixed error messages. It writes a private temporary file in
`/var/lib/aboutme/deployment`, checks the output bounds, sets mode `0644`, and
renames the file atomically to `deployment.json`. A lock serializes writers.

The directory is root-owned with mode `0755`. Serving and maintenance Caddy
mount that directory read-only at `/srv/deployment`. A directory mount keeps
atomic replacements visible. The direct edge serves only the exact document path
for GET and HEAD. Other methods get 405; a missing file gets 404. Responses
carry `Content-Type: application/json`, `Cache-Control: public, max-age=30`,
`Access-Control-Allow-Origin: *`, HSTS `max-age=31536000`, and
`X-Content-Type-Options: nosniff`. Caddy removes its `Server` and `Via` headers.
The `www` host keeps its apex redirect.

## Evidence and compatibility

The document uses `platform.provider: greennode`,
`platform.orchestrator: podman`, and region `HCM03`. These existing fields
identify the host reporter without a new field. Version 1 adds the `podman` enum
value without changing field meanings or accepting extra properties. Existing
ECS and Kubernetes documents retain their schema and behavior. Older pages that
reject the new enum need a reload after the updated web image ships.

The host reporter publishes signatures and SBOMs as `unchecked`. It does not
copy deploy-time verification or release metadata into runtime evidence. The
version, commit, signed links, and release stay null. A complete host report
therefore shows the running digests with the page's Unverified state. The
existing GitHub attestation and Sigstore verifier and independent commands
remain unchanged. Automatic host verification requires integration with that
verifier; a shell implementation must not invent a second verification policy.

The page names the reporter in Vietnamese and English. It states that the host
reports its own Podman state. The report does not prove host or operator
honesty, runtime integrity, configuration, secrets, or source quality. Only an
image whose signature was checked may carry a verified build claim.

AWS ECS keeps its off-host observer, S3 document, CloudFront route, and
verification cache. Kubernetes keeps its read-only pod adapter and the same
verifier. Neither platform depends on the host reporter or its Caddy route.

## Operation and rollback

Host installation installs and enables the timer and creates the public
directory before either Caddy container starts. Refreshing the report does not
start, stop, or change an application container. The timer also observes manual
restarts and drift after deploys. A service failure uses the host's existing
alert service.

Disabling the timer leaves the last document to become stale. Removing the
public file produces 404. Rolling back the web image can make the page refuse
the Podman enum; the machine document remains available for manual checks. No
database or resume data changes, and overwriting a snapshot loses no stored
history.
