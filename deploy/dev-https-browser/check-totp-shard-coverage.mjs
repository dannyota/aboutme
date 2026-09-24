// Checks that every step of the TOTP second-factor scenario is proven
// exactly once across the totp-browser-proof shard matrix (totp.spec.ts
// "Enabled-proof sharding"; run by the totp-browser-proof-coverage job after
// every shard finishes). Each evidence file was already checked against the
// closed step-name list by verify-evidence.mjs when its own shard ran; this
// checks that the shard-to-role assignment is complete, that evidence for
// every shard (and only every shard) is present once, that the union of
// enabled-phase evidence covers every step, and that exactly one
// disabled-phase evidence file is present.
//
// verify-evidence.mjs is a CLI script, not an importable module (its own
// top-level code reads process.argv), so this list is its own copy of that
// file's TOTP_STEP_NAMES. totp.spec.ts owns the shard-to-role map this file
// mirrors as TOTP_SHARD_ROLES. Keep all three in sync.
import { readFile } from 'node:fs/promises';

const TOTP_STEP_NAMES = new Set([
  'agentGranted', 'agentRevoked', 'attemptsExhausted', 'cleanup',
  'concurrentUseRejected', 'currentStepAccepted', 'enrolled', 'finalRemoved',
  'invalidCodeRejected', 'locales', 'nextStepAccepted', 'oneRemoved',
  'otherSessionRevoked', 'otherSessionStarted', 'passkeyCoexistence',
  'passwordPending', 'previousStepAccepted', 'providerAccount',
  'providerPending', 'qrIsLocal', 'reauthRequired', 'recoveryCompletion',
  'recoveryRevealedOnce', 'replaced', 'resetPreservesEnforcement',
  'sameStepReplayRejected', 'supersededEnrollmentRejected',
  'unicodeDigitsRejected', 'viewports', 'wrongEpochRejected',
  'wrongSessionRejected',
]);

// Mirrors totp.spec.ts's TOTP_SHARD_ROLES.
const TOTP_SHARD_ROLES = {
  'primary': ['primary'],
  'skew': ['skew'],
  'replay-concurrent': ['replay', 'concurrent'],
  'replace-recovery': ['replace', 'recovery'],
  'locale-attempts': ['locale', 'attempts'],
  'epoch-disabled': ['epoch'],
};

// Every AccountRole the enabled journey proves, excluding 'none' (no account
// yet) and 'disabled' (the separate disabled-enrollment phase, proven by the
// epoch-disabled shard's disabled-phase run instead).
const ENABLED_ROLES = [
  'primary', 'skew', 'replay', 'concurrent', 'replace', 'epoch', 'locale',
  'recovery', 'attempts',
];

function assertShardRoleMapIsComplete() {
  const assignedTo = new Map();
  for (const [shard, roles] of Object.entries(TOTP_SHARD_ROLES)) {
    for (const role of roles) {
      if (assignedTo.has(role)) {
        throw new Error(
          `role ${role} is assigned to both ${assignedTo.get(role)} and ${shard}`,
        );
      }
      assignedTo.set(role, shard);
    }
  }
  const missing = ENABLED_ROLES.filter((role) => !assignedTo.has(role));
  if (missing.length > 0) {
    throw new Error(`TOTP_SHARD_ROLES does not assign a shard to: ${missing.join(', ')}`);
  }
}

/** The shard whose artifact directory a downloaded evidence path sits under. */
function shardOf(path) {
  for (const shard of Object.keys(TOTP_SHARD_ROLES)) {
    if (path.includes(`totp-browser-proof-evidence-${shard}/`)) return shard;
  }
  return null;
}

async function main() {
  const paths = process.argv.slice(2);
  if (paths.length === 0) {
    throw new Error('usage: check-totp-shard-coverage.mjs <evidence-path>...');
  }

  assertShardRoleMapIsComplete();

  const enabledPaths = paths.filter((p) => p.endsWith('totp-second-factor-proof.json'));
  const disabledPaths = paths.filter((p) => p.endsWith('totp-enrollment-disabled-proof.json'));
  if (enabledPaths.length + disabledPaths.length !== paths.length) {
    throw new Error('every path must be a totp-second-factor-proof.json or totp-enrollment-disabled-proof.json file');
  }

  const seenShards = new Set();
  const proven = new Set();
  for (const path of enabledPaths) {
    const shard = shardOf(path);
    if (shard === null) {
      throw new Error(`cannot determine which shard produced this evidence file: ${path}`);
    }
    if (seenShards.has(shard)) {
      throw new Error(`more than one enabled evidence file for shard ${shard}`);
    }
    seenShards.add(shard);
    const body = JSON.parse(await readFile(path, 'utf8'));
    for (const [name, value] of Object.entries(body.steps ?? {})) {
      if (value === true && TOTP_STEP_NAMES.has(name)) proven.add(name);
    }
  }

  const expectedShards = Object.keys(TOTP_SHARD_ROLES).sort();
  const gotShards = [...seenShards].sort();
  if (expectedShards.join(',') !== gotShards.join(',')) {
    throw new Error(
      `enabled evidence covers shards [${gotShards.join(', ')}], expected exactly [${expectedShards.join(', ')}]`,
    );
  }

  const missingSteps = [...TOTP_STEP_NAMES].filter((name) => !proven.has(name)).sort();
  if (missingSteps.length > 0) {
    throw new Error(`missing steps: ${missingSteps.join(', ')}`);
  }

  if (disabledPaths.length !== 1) {
    throw new Error(`expected exactly one disabled-phase evidence file, found ${disabledPaths.length}`);
  }

  console.log(
    `totp shard coverage: all ${TOTP_STEP_NAMES.size} steps proven across ${seenShards.size} shard(s); disabled phase proven once`,
  );
}

try {
  await main();
} catch (err) {
  console.error(`totp shard coverage: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
}
