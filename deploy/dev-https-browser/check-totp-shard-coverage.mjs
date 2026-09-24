// Checks that every step of the TOTP second-factor scenario is true
// somewhere across a set of per-shard evidence files (totp.spec.ts
// "Enabled-proof sharding"; run by the totp-browser-proof-coverage job after
// every totp-browser-proof shard finishes). Each file was already checked
// against the closed step-name list by verify-evidence.mjs when its own
// shard ran; this only checks the union covers every step.
//
// verify-evidence.mjs is a CLI script, not an importable module (its own
// top-level code reads process.argv), so this list is its own copy of that
// file's TOTP_STEP_NAMES. Keep the two in sync.
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

const paths = process.argv.slice(2);
if (paths.length === 0) {
  console.error('usage: check-totp-shard-coverage.mjs <evidence-path>...');
  process.exit(1);
}

const proven = new Set();
for (const path of paths) {
  const body = JSON.parse(await readFile(path, 'utf8'));
  for (const [name, value] of Object.entries(body.steps ?? {})) {
    if (value === true && TOTP_STEP_NAMES.has(name)) proven.add(name);
  }
}

const missing = [...TOTP_STEP_NAMES].filter((name) => !proven.has(name)).sort();
if (missing.length > 0) {
  console.error(`totp shard coverage: missing steps: ${missing.join(', ')}`);
  process.exit(1);
}
console.log(
  `totp shard coverage: all ${TOTP_STEP_NAMES.size} steps proven across ${paths.length} shard file(s)`,
);
