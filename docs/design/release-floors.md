# Release floors for rebuilt hosts and schema changes

Status: accepted, 2026-11-12. Implemented for the deploy and fence; no tree has
a schema floor yet. Owns where the Vietnam host's release floor comes from on a
rebuilt host and how a migration that breaks older releases raises it. The fence
itself, its lock, and its checks are in
[Vietnam production](vietnam-production.md#release-fence).

## Tracked floor

`deploy/vn/scripts/fence.sh` names the tracked floor (`floor_tag`). A rebuilt
host's `fence.sh init` starts there. A restored backup can carry any state
production had, so a rebuilt host must refuse every release production refuses.
The tracked floor is therefore never below production's floor.

`fence.sh raise` refuses a tag above the tracked floor of the installed
`fence.sh`. Every floor change goes through `raise`: `deploy.sh --activate`,
CrowdSec notice activation, and the schema-floor raise below. Raising
production's floor therefore first lands a reviewed change that raises the
tracked floor, then installs the host bundle. The deploy's helper-hash check
holds the host's `fence.sh` to the deploying checkout.

The tracked floor names only a release whose images are published and verified.
It can sit above production's floor; a rebuilt host then needs a release at or
above it, and restores an older backup by migrating it forward under that
release. A restore never lowers a fence: neither a rebuilt host nor an in-place
point-in-time restore goes below the floor it started with.

## Schema floor

A migration can make older releases unsafe, for example by dropping a column
they read. `apps/server/migrations/schema-floor` holds one strict tag: the
lowest published release that runs against the schema after that tree's
migrations. Goose embeds only `*.sql`, so the file is not a migration. A
breaking change ships in two releases. The first stops depending on what the
migration removes and runs against both schemas; it is published and verified
first. A later change adds the migration and raises this file and the tracked
floor together to that published release. For the showcase review columns, a
release whose queries name only the kept columns ships first, and the later drop
names that release as its schema floor. sqlc expands `*` and `alias.*` into the
full column list when it generates code, so a released binary names every column
even where the source says `*`. The first release therefore converts each `*`
query on the affected table to an explicit list and adds a static test that no
generated query names the removed columns. A static test keeps the tracked floor
at or above the file.

The deploy reads the file from the candidate's commit, which the release
eligibility check binds to the tag, never from the checkout. It refuses a
candidate below its own schema floor before the lock. When the file names a
release above the fence, the deploy raises the fence after the backup, after
maintenance is confirmed and the app is down, and before `db-setup` or
`migrate`. It marks the deploy non-restorable before sending the raise, as it
does before a migration, so a lost reply or a failed raise fails closed instead
of starting the previous release, which the raised fence may refuse. `migrate`
runs only after the raise succeeds. A later failed start leaves the floor
raised, so neither recovery nor a rollback starts a release that reads the old
schema.

A tree without the file, as in releases before this rule, adds no schema floor
and erases none. Absence is not evidence of compatibility: only the fence
protects such trees. Rollbacks run no migration and read no file; the fence
alone limits them. A candidate's file cannot lower the fence, because `raise`
refuses decreases.
