# Deployment document on the Vietnam host

The GreenNode vServer reports its running Podman containers to an observer
container, which checks their signed build records and publishes
`/.well-known/deployment.json`. The root reporter and observer share the host.
The report is weaker evidence than the AWS ECS observer's platform API report
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
Maintenance appears only while running. The previous observation remains on a
failed read. The observer rejects an observation older than 180 seconds or more
than 60 seconds in the future, writes nothing, and lets the public document age
into stale.

The reporter builds the [document](document.md) from constants and the four
allowed observation fields. It never reads container environment values or
secret files. It uses formatted Podman output, discards command error output,
and logs only fixed error messages. It writes a private temporary file in
`/var/lib/aboutme/observation`, checks the output bounds, sets mode `0644`, and
renames the file atomically to `observation.json`. A lock serializes writers.
The root-owned directory has mode `0755`.

The observer reads that directory at `/observation` through a read-only mount.
It validates the closed document schema, the component order, replica bounds,
digests, and RFC 3339 timestamps, then passes only runtime observations to the
existing aggregator and Sigstore verifier. The reporter's unchecked build
claims never become verification evidence. Reads are capped at 64 KiB.
The public document keeps the root reporter's `observed_at`; verification
cannot extend a snapshot's freshness.

The observer publishes to `/var/lib/aboutme/deployment`, owned by its uid
`65532` with mode `0755`. It writes a temporary file beside `deployment.json`,
sets mode `0644`, fsyncs and closes the file, renames it, and fsyncs the
directory. A failure before rename removes the temporary file and preserves
the previous document. A directory fsync failure after rename is logged as an
error; the complete new document may be visible.

Serving and maintenance Caddy mount the public directory read-only at
`/srv/deployment`. A directory mount keeps atomic replacements visible. The
direct edge serves only the exact document path for GET and HEAD. Other methods
get 405; a missing file gets 404. Responses
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

The root reporter writes signatures and SBOMs as `unchecked` and leaves build
claims null. The observer checks each running digest with the existing
[verification policy](verification.md#what-verified-means), using `sigstore-go`.
It derives versions, commits, signed links, and the release from those checks.
A fresh document whose signatures pass shows Verified; an unfinished check
shows Unverified. Deploy-time checks and `release.json` are not runtime
evidence. The independent commands use the same policy on every platform.

The page names the reporter in Vietnamese and English. It states that the host
reports its own Podman state and an observer container on that host checks the
signatures. The report does not prove host or operator honesty, runtime
integrity, configuration, secrets, or source quality. Only an image whose
signature was checked may carry a verified build claim.

AWS ECS keeps its off-host observer, S3 document, CloudFront route, and
verification cache. Kubernetes keeps its read-only pod adapter and the same
verifier. Neither platform depends on the host reporter or its Caddy route.

## Operation and rollback

Host installation installs and enables the root reporter's timer, creates both
directories, and installs the observer Quadlet template. Deploy verifies the
single-platform `aboutme-observer:<tag>-amd64` image's provenance, pulls it by
digest, renders the unit, and starts or restarts it. A tag without that image
leaves the installed observer unit unchanged and reports that choice.

The observer runs `observer run --interval 60s` as uid `65532` with a read-only
root filesystem, all capabilities dropped, no-new-privileges, no secrets, and
a 128 MiB memory cap. Its TUF cache is a bounded tmpfs. The bridge network gives
it outbound HTTPS for GitHub, the registry fallback, and Sigstore TUF; it has no
published ports or Podman socket. Verification results stay in memory for the
process lifetime and retain the daily recheck policy. No bucket is required.

The reporter and observer keep running during maintenance. Refreshing evidence
does not start, stop, or change application containers. The timer observes
manual restarts and drift. A service failure uses the host's alert service.

Disabling the timer or observer leaves the last document to become stale.
Removing the public file produces 404. Rolling back the web image can make the
page refuse the Podman enum; the machine document remains available for manual
checks. No database or resume data changes, and overwriting a snapshot loses no
stored history.
