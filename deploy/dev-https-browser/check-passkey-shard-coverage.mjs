// Checks that every step of the passkey second-factor scenario is proven
// exactly once across the passkey-browser-proof shard matrix
// (second-factor.spec.ts "Enabled-proof sharding"; run by the
// passkey-browser-proof-coverage job after every shard finishes). Each
// evidence file was already checked against the closed step-name list by
// verify-evidence.mjs when its own shard ran; this checks that the
// shard-to-role assignment is complete, that evidence for every shard (and
// only every shard) is present once, that the union of enabled-phase
// evidence covers every step, and that exactly one disabled-phase evidence
// file is present.
//
// verify-evidence.mjs is a CLI script, not an importable module (its own
// top-level code reads process.argv), so this list is its own copy of that
// file's PASSKEY_STEP_NAMES. second-factor.spec.ts owns the shard-to-role
// map this file mirrors as PASSKEY_SHARD_ROLES. Keep all three in sync.
import { readFile } from 'node:fs/promises';

const PASSKEY_STEP_NAMES = new Set([
  'agentGranted', 'agentRevoked', 'attemptsExhausted', 'ceremonyReplayRejected',
  'cleanup', 'concurrentCompletion', 'enrolled', 'finalRemoved', 'locales',
  'oneRemoved', 'otherSessionRevoked', 'otherSessionStarted',
  'passkeyCompletion', 'passwordPending', 'providerAccount', 'providerPending',
  'reauthPending', 'reauthRequired', 'recoveryCompletion',
  'recoveryRegenerated', 'recoveryRevealedOnce', 'recoveryReuseRejected',
  'resetPreservesEnforcement', 'secondPasskeyAdded', 'staleEpochRejected',
  'userVerificationRequired', 'viewports', 'wrongBindingRejected',
  'wrongOriginRejected',
]);

// Mirrors second-factor.spec.ts's PASSKEY_SHARD_ROLES.
const PASSKEY_SHARD_ROLES = {
  'primary-disabled': ['primary'],
  'recovery-attempts': ['recovery', 'attempts'],
};

// Every AccountRole the enabled journey proves, excluding 'none' (no account
// yet) and 'disabled' (the separate disabled-enrollment phase, proven by the
// primary-disabled shard's disabled-phase run instead).
const ENABLED_ROLES = ['primary', 'recovery', 'attempts'];

function assertShardRoleMapIsComplete() {
  const assignedTo = new Map();
  for (const [shard, roles] of Object.entries(PASSKEY_SHARD_ROLES)) {
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
    throw new Error(`PASSKEY_SHARD_ROLES does not assign a shard to: ${missing.join(', ')}`);
  }
}

/** The shard whose artifact directory a downloaded evidence path sits under. */
function shardOf(path) {
  for (const shard of Object.keys(PASSKEY_SHARD_ROLES)) {
    if (path.includes(`passkey-browser-proof-evidence-${shard}/`)) return shard;
  }
  return null;
}

async function main() {
  const paths = process.argv.slice(2);
  if (paths.length === 0) {
    throw new Error('usage: check-passkey-shard-coverage.mjs <evidence-path>...');
  }

  assertShardRoleMapIsComplete();

  const enabledPaths = paths.filter((p) => p.endsWith('passkey-second-factor-proof.json'));
  const disabledPaths = paths.filter((p) => p.endsWith('passkey-enrollment-disabled-proof.json'));
  if (enabledPaths.length + disabledPaths.length !== paths.length) {
    throw new Error('every path must be a passkey-second-factor-proof.json or passkey-enrollment-disabled-proof.json file');
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
      if (value === true && PASSKEY_STEP_NAMES.has(name)) proven.add(name);
    }
  }

  const expectedShards = Object.keys(PASSKEY_SHARD_ROLES).sort();
  const gotShards = [...seenShards].sort();
  if (expectedShards.join(',') !== gotShards.join(',')) {
    throw new Error(
      `enabled evidence covers shards [${gotShards.join(', ')}], expected exactly [${expectedShards.join(', ')}]`,
    );
  }

  const missingSteps = [...PASSKEY_STEP_NAMES].filter((name) => !proven.has(name)).sort();
  if (missingSteps.length > 0) {
    throw new Error(`missing steps: ${missingSteps.join(', ')}`);
  }

  if (disabledPaths.length !== 1) {
    throw new Error(`expected exactly one disabled-phase evidence file, found ${disabledPaths.length}`);
  }

  console.log(
    `passkey shard coverage: all ${PASSKEY_STEP_NAMES.size} steps proven across ${seenShards.size} shard(s); disabled phase proven once`,
  );
}

try {
  await main();
} catch (err) {
  console.error(`passkey shard coverage: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
}
