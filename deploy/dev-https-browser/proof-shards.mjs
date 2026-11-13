// The enabled-proof shard maps of the two sharded second-factor journeys,
// each shard name mapped to the account roles it proves. The specs pick their
// roles from these maps ("Enabled-proof sharding" in totp.spec.ts and
// second-factor.spec.ts) and check-shard-coverage.mjs checks every shard's
// evidence against them. run.sh validates the shard names in shell with its
// own lists. A CI job runs one passkey shard, or several TOTP shards as
// parallel tests against one harness; each TOTP shard's roles use only their
// own fictional accounts, so shards never share an account.

export const TOTP_SHARD_ROLES = Object.freeze({
  'primary': ['primary'],
  'skew': ['skew'],
  'replay-concurrent': ['replay', 'concurrent'],
  'replace-recovery': ['replace', 'recovery'],
  'locale-attempts': ['locale', 'attempts'],
  'epoch-disabled': ['epoch'],
});

export const TOTP_ENABLED_ROLES = Object.freeze([
  'primary', 'skew', 'replay', 'concurrent', 'replace', 'epoch', 'locale',
  'recovery', 'attempts',
]);

export const PASSKEY_SHARD_ROLES = Object.freeze({
  'primary-disabled': ['primary'],
  'recovery-attempts': ['recovery', 'attempts'],
});

export const PASSKEY_ENABLED_ROLES = Object.freeze([
  'primary', 'recovery', 'attempts',
]);
