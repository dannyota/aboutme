// The enabled-proof shard maps of the two sharded second-factor journeys,
// each shard name mapped to the account roles it proves. The specs pick their
// roles from these maps ("Enabled-proof sharding" in totp.spec.ts and
// second-factor.spec.ts) and check-shard-coverage.mjs checks every shard's
// evidence against them. run.sh and scripts/dev-https-check.sh validate the
// shard names in shell with their own lists.

export const TOTP_SHARD_ROLES = Object.freeze({
  'primary': ['primary'],
  'skew': ['skew'],
  'replay-concurrent': ['replay', 'concurrent'],
  'replace-recovery': ['replace', 'recovery'],
  'locale-attempts': ['locale', 'attempts'],
  'epoch-disabled': ['epoch'],
});

// Every role the enabled TOTP journey proves, excluding 'none' (no account
// yet) and 'disabled' (the separate disabled-enrollment phase, proven by the
// epoch-disabled shard's disabled-phase run instead).
export const TOTP_ENABLED_ROLES = Object.freeze([
  'primary', 'skew', 'replay', 'concurrent', 'replace', 'epoch', 'locale',
  'recovery', 'attempts',
]);

export const PASSKEY_SHARD_ROLES = Object.freeze({
  'primary-disabled': ['primary'],
  'recovery-attempts': ['recovery', 'attempts'],
});

// Every role the enabled passkey journey proves, excluding 'none' and
// 'disabled' (proven by the primary-disabled shard's disabled-phase run).
export const PASSKEY_ENABLED_ROLES = Object.freeze([
  'primary', 'recovery', 'attempts',
]);
