# Vietnam host operations recovery

This page holds the quiescent repair and recovery archive contracts of the
[host operations](vietnam-host-operations.md) design and its
[delivery companion](vietnam-host-operations-delivery.md).

## Quiescent repair

This is a privileged host procedure for a rotation that cannot serve, not a new
public command or ordinary `rebind`. It ships with TOTP rotation and applies to
view-pass rotation when that feature ships. The operator runs a reviewed repair
bundle from clean, current reviewed `main` over pinned SSH as `aboutme-admin`
with sudo, independently of the broken runner. The repair may replace only the
reviewed operation helpers/units and bind a compatible forward image. It must
support the saved version-1 protocol and existing database schema; no migration,
DB restore, key generation, credential reset, or general installer runs here.

1. Verify the repair bundle's exact hashes, independent review and green CI;
   verify any image's normal tag eligibility, provenance and amd64 digests. The
   image must meet every current floor and enabled flag, and the tested rotation
   contract allowlist. No healthy live release or successful suspension is an
   entry condition. Unknown/malformed state requires a separately reviewed
   recovery; this path never guesses a missing key or marker owner.
2. Stop the old request unit before taking its flock; never treat stop success
   as quiescence. Take the request flock and fence as its same owner, drain
   jobs, then take the workload lock. Stop serving units and all matching
   containers; prove all matching operation and serving DB backends absent. Keep
   timers stopped and prove external maintenance 503. Take a root-only
   checkpoint of request state, fence, marker, flags and key metadata. Preserve
   key bytes, staging, archives and recovery receipts in place, without emitting
   values. Retain the key marker, or create one for this request before code or
   binding changes. Keep it through repair; ordinary starts and installation
   stay blocked.
3. Fsync a repair intent in the request with old/new release records, bundle
   hashes, reason, UTC and stage (`installing|bound|verified`). Retain attempt
   history, target rotation, fence owner, markers, flags and release floors.
   Install only the reviewed files while quiescent; verify all installed hashes
   before running any repaired helper. Interrupted installation stays fenced.
4. If the image changes, reconcile the request, fence tag and existing marker's
   release binding to the new record under that intent. Partial binding blocks
   every start; recovery uses the intent to finish the same binding, never clear
   ownership. Keep the old record as evidence. Invalidate proof nonces and zero
   snapshots and restart the TOTP wait. A bundle-only repair keeps the image.
5. The repaired owning runner resumes the saved rotation forward with the same
   keys and target ring. Only its normal file/secret/unit consistency checks may
   remove a key marker. Then use `reload-live` and fresh readiness, ring and
   external serving checks. No repair step bypasses a marker to start an image.

The operator records `verified` only after installed hashes and running digests
match the repair intent, configuration matches the intended ring/key, and fresh
action-specific serving proof passes (or view-pass closes `unverified` under its
lost-witness rule). TOTP re-encryption, zero snapshots, wait and retirement
still finish through normal resume/retire. Repair success never implies rotation
completion. Failure retains maintenance and the fence; a retry adopts the same
intent after quiescence. No automatic rollback, fence clear or state deletion.

## Recovery archive

TOTP rotation ships this versioned format; deletion needs no archive or age
recipient. `operation-copy` normalizes only TOTP and view keys from current
files, staging and archives into an age-encrypted, uncompressed tar. Plaintext
contains exactly `manifest.json` and the regular files it lists, no host paths,
other credentials, database, request audit or identity. Key files contain
exactly 43 ASCII base64url bytes without a newline. Directories are implicit.
Limit the manifest to 1 MiB, keys to 4096 entries, and decrypted tar to 8 MiB;
refuse export overflow without pruning keys. Manifest JSON has exactly these
required fields:

| Field                       | Contract                                                                                                                                             |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `schemaVersion`             | Integer 1. Unknown versions refuse restore.                                                                                                          |
| `requestId`, `kind`         | Existing request grammar; kind is `totp-rotate` or `view-pass-rotate`.                                                                               |
| `createdAt`, `purpose`      | UTC RFC3339; `switch` or `retire` (TOTP only).                                                                                                       |
| `release`, `minimumRelease` | Closed saved release-json; nonnegative integer host floor at export.                                                                                 |
| `totpBefore`, `totpAfter`   | Each exactly `{active,previous}`; active is `{slot,keyId}`, previous is that shape or null. Slot is `a` or `b`; ID uses the TOTP derived-ID grammar. |
| `viewKeys`                  | Array of exactly `{sha256,restoreState}`; state is always `retired`. Includes current, staged and archived view keys.                                |
| `files`                     | Array of exactly `{path,sha256}`; digests are 64 lowercase hex over the normalized file bytes.                                                       |

TOTP paths are exactly `keys/totp/<derived-id>`; view paths are exactly
`keys/view/<sha256>`. Sort files by path and `viewKeys` by digest; reject
duplicates. Every ring/view reference must name a file; every view file needs a
`viewKeys` entry. Unreferenced TOTP files are historical keys, never implicit
active slots. For a TOTP switch, `totpBefore` is the saved old-only ring and
`totpAfter` is staged active plus old previous. For retirement, before is the
two-key ring and after removes previous. View rotation keeps both TOTP ring
records identical. Slot and ID must be distinct within a ring; the union
contains at most two TOTP IDs, each mapped to the same slot across both rings.
The manifest describes intent, not proof of activation. `restoreState:retired`
forbids loading any exported view key on restore, even if still live or staged
when exported. Activation itself continues to use the original request state.
Export validates this contract against the saved request, release and staged key
ID before encryption. Every `sha256` field uses 64 lowercase hex characters.

Restore is an operator-run, reviewed host recovery under maintenance. Decrypt
through a process using the separately held age identity, never through model
context, logs or the repository. Use a fresh root-only destination and validate
the complete archive before installing anything: age authentication, closed JSON
without unknown or duplicate fields, sizes, canonical encoding, digests, derived
TOTP IDs, ring transitions and exact file set. Reject links, special files,
absolute/traversal paths, duplicate tar entries and unlisted/trailing content;
never extract over live files. Restore modes are directories 0700, keys 0400.

Inventory distinct key IDs from **all** restored TOTP credential and enrollment
rows, including expired enrollments, without printing row data. Select a
retained manifest whose before/after union covers that inventory; timestamps and
current slot filenames never establish precedence. Install `totpAfter.active`,
including its staged bytes when activation was partial, and map the union's
other key as previous in its recorded slot. This deliberately retains both keys
even for a retirement export. A pre-switch DB can also move forward through this
ring. More than two row IDs, missing keys or no matching manifest stops
recovery; historical TOTP files require their matching retained manifest, not a
guessed mapping. Do not merge conflicting manifests or rewrite factors to make
them fit.

Generate a fresh view key on the restored host; every historical view key stays
retired. The manifest never overwrites surviving request state, markers, flags
or floors, and never authorizes replay of an interrupted request. Resolve those
under the reviewed recovery before serving. Use an eligible schema-compatible
image at or above the current host floor, manifest floor and all enabled-flag
floors; loss of current floor authority requires reviewed reconstruction before
serving, not adoption of the archived floor. Start a fresh ten-minute wait when
the restored ring becomes active; reboot restarts it. Prove normal
re-encryption, both zero snapshots and TOTP sign-in before retiring the previous
key. Require readiness and external serving proofs; retain both keys on failure.
