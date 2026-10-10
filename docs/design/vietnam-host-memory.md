# Vietnam host memory and storage

The Vietnam production host has 2 vCPU and 4 GiB of RAM. These limits are
planning ceilings, not reservations or measured usage. Rehearsal measures real
peaks before cutover. A failed memory gate means resizing the host or reducing
concurrency, not raising a limit without evidence.

## Memory budget

| Consumer                            | Hard ceiling | Concurrency rule                                                |
| ----------------------------------- | -----------: | --------------------------------------------------------------- |
| Go and Chromium                     |      512 MiB | One server container                                            |
| Nuxt                                |      256 MiB | One web container                                               |
| Caddy                               | 128 MiB each | One normally; serving and maintenance may overlap during deploy |
| PostgreSQL                          |        1 GiB | One host service                                                |
| CrowdSec                            |      256 MiB | One host service                                                |
| CrowdSec firewall bouncer           |       64 MiB | One host service                                                |
| App job, migrate, or database setup |      256 MiB | At most one of this group                                       |
| Caddy log tmpfs                     |       64 MiB | One host mount shared by both Caddy processes                   |

The largest normal service envelope is 2.5 GiB: PostgreSQL, Go, Nuxt, one Caddy,
CrowdSec, the firewall bouncer, one 256 MiB job, and a full 64 MiB log tmpfs.
This leaves 1.5 GiB outside those ceilings. The plan allows 1 GiB for the
kernel, systemd, Podman, journald, SSH, native pgBackRest work, and useful page
cache. The remaining 512 MiB is shared by physical zram use and safety margin.
Rehearsal requires at least 256 MiB unused at the combined peak after counting
zram. These allowances are not reservations.

Zram has a 512 MiB logical ceiling. It consumes physical RAM as compressed pages
fill it and replaces resident pages; it is not extra capacity. The gate records
its physical use once and rejects a peak that consumes the safety margin. There
is no disk swap.

The Caddy log tmpfs has a hard 64 MiB size. WAF logs can hold 30 MiB: two
streams, each with a 5 MiB current file and two rolls. CrowdSec request and
bouncer logs can hold 8 MiB: four streams, each with a 1 MiB current file and
one roll. The remaining 26 MiB covers filesystem metadata and rotation overlap.
The privacy retention unit still removes expired records; the size limit is a
memory bound, not the retention authority.

All scheduled app jobs share one lock and run one at a time. A deploy stops
their timers before starting maintenance, so the second 128 MiB Caddy replaces
the 256 MiB job in the overlap envelope. The server and serving Caddy stop
before a migration or database setup starts. The deploy proof must show no job,
migration, or database setup overlaps server warmup. Restore drills use their
temporary host.

PostgreSQL has `shared_buffers = 256MB`, `work_mem = 4MB`,
`maintenance_work_mem = 64MB`, and `max_connections = 40` inside its 1 GiB
service ceiling. `work_mem` applies to each sort or hash operation and can be
used several times by one query or parallel worker. Multiplying it once by the
connection count is not a memory bound. `effective_cache_size` is a planner
estimate and allocates no memory. The API and each job can configure a 20
connection store pool, so implementation must not lower the database limit
without proving every pool, backup, and admin allowance. Rehearsal exercises API
traffic and one job together and records connection exhaustion.

## Rehearsal gate

The rehearsal records cgroup current and peak memory, `memory.events`, zram use,
swap input and output, pressure stall information, the kernel OOM journal, and
tmpfs high-water use. It covers:

- ordinary serving at the expected concurrency;
- concurrent Chromium print work;
- one scheduled job and a PostgreSQL backup;
- maintenance startup while serving still answers; and
- migration and database setup after serving stops.

Cutover requires no kernel or cgroup out-of-memory kill, no tmpfs exhaustion, no
sustained memory pressure, and enough unused memory for the host allowance. The
evidence records each service peak, native pgBackRest use, physical zram use,
and the combined peak. Less than 256 MiB unused at the combined peak, a
connection-pool failure, or a migration failure blocks cutover.

## Storage

The root disk is 30 GB, encrypted, and holds only Ubuntu, public container
images, package caches, and Podman metadata. The encrypted data volume starts at
20 GB and holds PostgreSQL, secrets, CrowdSec state, Caddy state, and the
journal. Caddy security logs remain on the 64 MiB tmpfs.

The 30 GB root allows deploy overlap for the server, web, and Caddy images plus
operating-system updates without placing personal data there. The 20 GB data
volume is sufficient for the current database and journal only as a starting
size. Existing 70 percent alarms trigger online growth for either volume.
