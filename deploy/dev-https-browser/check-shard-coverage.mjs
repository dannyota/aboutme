// Checks that every step of a sharded second-factor scenario is proven
// across its CI shard matrix, after every shard finishes: `totp` for the
// totp-browser-proof matrix and `passkey` for passkey-browser-proof. Each
// evidence file was already checked against the closed step-name list by
// verify-evidence.mjs when its own shard ran; this checks that the
// shard-to-role map is complete, that evidence for every shard (and only
// every shard) is present once, that the union of enabled-phase evidence
// covers every step, and that exactly one disabled-phase evidence file is
// present. The step lists come from verify-evidence.mjs and the shard maps
// from proof-shards.mjs.
//
// Usage: node check-shard-coverage.mjs <totp|passkey> <evidence-path>...
import { realpathSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import {
  PASSKEY_ENABLED_ROLES,
  PASSKEY_SHARD_ROLES,
  TOTP_ENABLED_ROLES,
  TOTP_SHARD_ROLES,
} from './proof-shards.mjs';
import { PASSKEY_STEP_NAMES, TOTP_STEP_NAMES } from './verify-evidence.mjs';

// Evidence and artifact names start with the scenario name:
// <name>-second-factor-proof.json, <name>-enrollment-disabled-proof.json,
// and one <name>-browser-proof-evidence-<shard>/ artifact per shard.
const SCENARIOS = {
  passkey: {
    enabledRoles: PASSKEY_ENABLED_ROLES,
    mapName: 'PASSKEY_SHARD_ROLES',
    shardRoles: PASSKEY_SHARD_ROLES,
    steps: PASSKEY_STEP_NAMES,
  },
  totp: {
    enabledRoles: TOTP_ENABLED_ROLES,
    mapName: 'TOTP_SHARD_ROLES',
    shardRoles: TOTP_SHARD_ROLES,
    steps: TOTP_STEP_NAMES,
  },
};

function assertShardRoleMapIsComplete({ enabledRoles, mapName, shardRoles }) {
  const assignedTo = new Map();
  for (const [shard, roles] of Object.entries(shardRoles)) {
    for (const role of roles) {
      if (assignedTo.has(role)) {
        throw new Error(
          `role ${role} is assigned to both ${assignedTo.get(role)} and ${shard}`,
        );
      }
      assignedTo.set(role, shard);
    }
  }
  const missing = enabledRoles.filter((role) => !assignedTo.has(role));
  if (missing.length > 0) {
    throw new Error(`${mapName} does not assign a shard to: ${missing.join(', ')}`);
  }
}

/** The shard whose artifact directory a downloaded evidence path sits under. */
function shardOf(name, shardRoles, path) {
  for (const shard of Object.keys(shardRoles)) {
    if (path.includes(`${name}-browser-proof-evidence-${shard}/`)) return shard;
  }
  return null;
}

/** Checks one scenario's evidence paths; returns the summary or throws. */
async function checkCoverage(name, paths) {
  const scenario = SCENARIOS[name];
  const { shardRoles, steps } = scenario;
  if (paths.length === 0) {
    throw new Error(
      `usage: check-shard-coverage.mjs ${name} <evidence-path>...`,
    );
  }

  assertShardRoleMapIsComplete(scenario);

  const enabledFile = `${name}-second-factor-proof.json`;
  const disabledFile = `${name}-enrollment-disabled-proof.json`;
  const enabledPaths = paths.filter((p) => p.endsWith(enabledFile));
  const disabledPaths = paths.filter((p) => p.endsWith(disabledFile));
  if (enabledPaths.length + disabledPaths.length !== paths.length) {
    throw new Error(`every path must be a ${enabledFile} or ${disabledFile} file`);
  }

  const seenShards = new Set();
  const proven = new Set();
  for (const path of enabledPaths) {
    const shard = shardOf(name, shardRoles, path);
    if (shard === null) {
      throw new Error(`cannot determine which shard produced this evidence file: ${path}`);
    }
    if (seenShards.has(shard)) {
      throw new Error(`more than one enabled evidence file for shard ${shard}`);
    }
    seenShards.add(shard);
    const body = JSON.parse(await readFile(path, 'utf8'));
    for (const [step, value] of Object.entries(body.steps ?? {})) {
      if (value === true && steps.has(step)) proven.add(step);
    }
  }

  const expectedShards = Object.keys(shardRoles).sort();
  const gotShards = [...seenShards].sort();
  if (expectedShards.join(',') !== gotShards.join(',')) {
    throw new Error(
      `enabled evidence covers shards [${gotShards.join(', ')}], expected exactly [${expectedShards.join(', ')}]`,
    );
  }

  const missingSteps = [...steps].filter((step) => !proven.has(step)).sort();
  if (missingSteps.length > 0) {
    throw new Error(`missing steps: ${missingSteps.join(', ')}`);
  }

  if (disabledPaths.length !== 1) {
    throw new Error(`expected exactly one disabled-phase evidence file, found ${disabledPaths.length}`);
  }

  return `all ${steps.size} steps proven across ${seenShards.size} shard(s); disabled phase proven once`;
}

/** Runs the check for one scenario and exits 1 on any gap. */
export async function runShardCoverage(name, paths) {
  if (!Object.hasOwn(SCENARIOS, name)) {
    console.error('usage: check-shard-coverage.mjs <totp|passkey> <evidence-path>...');
    process.exit(1);
  }
  try {
    console.log(`${name} shard coverage: ${await checkCoverage(name, paths)}`);
  } catch (err) {
    console.error(`${name} shard coverage: ${err instanceof Error ? err.message : String(err)}`);
    process.exit(1);
  }
}

if (process.argv[1] !== undefined
  && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await runShardCoverage(process.argv[2] ?? '', process.argv.slice(3));
}
