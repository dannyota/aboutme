// Verifies a mode's /evidence JSON against the exact expected shape.
// Baked into the browser image (see Dockerfile); run.sh invokes it as
// `node verify-evidence.mjs <mode> <evidence-path>` after the browser proof
// exits, before printing PASS. Any mismatch, including an unexpected extra
// field, exits 1. check-shard-coverage.mjs imports the two step lists; only a
// direct run verifies evidence.
import { realpathSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

// A sharded totp-browser-proof run proves only some of this scenario's steps
// per shard (ABOUTME_TOTP_SHARD, set in the container environment by
// run.sh's --env when the host set it). This checks one shard's evidence
// against the closed step-name list below and leaves checking that every
// step is true somewhere to the aggregation the totp-browser-proof workflow
// runs after every shard finishes. An unset or empty shard is the unsharded
// case, which must still prove every step itself.
export const TOTP_STEP_NAMES = new Set([
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

// A sharded passkey-browser-proof run proves only some of this scenario's
// steps per shard (ABOUTME_PASSKEY_SHARD), the same as totp above: an unset
// or empty shard is the unsharded case, which must still prove every step.
export const PASSKEY_STEP_NAMES = new Set([
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

if (process.argv[1] !== undefined
  && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await verify(process.argv[2], process.argv[3]);
}

async function verify(mode, path) {
  const actual = JSON.parse(await readFile(path, 'utf8'));
  const common = {
    errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
    origin: 'https://localhost:20443',
  };

  if (mode === 'totp') {
    const { steps, ...rest } = actual;
    const expectedRest = {
      ...common,
      scenario: 'totp-second-factor',
      schemaVersion: 1,
    };
    const stepNames = Object.keys(steps ?? {});
    const stepsValid = stepNames.length > 0 && stepNames.every(
      (name) => TOTP_STEP_NAMES.has(name) && steps[name] === true,
    );
    const countValid = process.env.ABOUTME_TOTP_SHARD
      ? true
      : stepNames.length === TOTP_STEP_NAMES.size;
    if (
      JSON.stringify(rest) !== JSON.stringify(expectedRest)
      || !stepsValid || !countValid
    ) {
      process.exit(1);
    }
    process.exit(0);
  }

  if (mode === 'second-factor') {
    const { steps, ...rest } = actual;
    const expectedRest = {
      ...common,
      scenario: 'passkey-second-factor',
      schemaVersion: 1,
    };
    const stepNames = Object.keys(steps ?? {});
    const stepsValid = stepNames.length > 0 && stepNames.every(
      (name) => PASSKEY_STEP_NAMES.has(name) && steps[name] === true,
    );
    const countValid = process.env.ABOUTME_PASSKEY_SHARD
      ? true
      : stepNames.length === PASSKEY_STEP_NAMES.size;
    if (
      JSON.stringify(rest) !== JSON.stringify(expectedRest)
      || !stepsValid || !countValid
    ) {
      process.exit(1);
    }
    process.exit(0);
  }

  const expected = mode === 'auth' ? {
    ...common,
    scenario: 'google-authentication',
    schemaVersion: 1,
    steps: Object.fromEntries(
      Array.from({ length: 10 }, (_, index) => [String(index + 1), true]),
    ),
  } : mode === 'transport' ? {
    ...common,
    scenario: 'authenticated-transport',
    schemaVersion: 1,
    steps: { auth: true, cache: true, etag: true, ifMatch: true, teardown: true },
  } : mode === 'public' ? {
    schemaVersion: 1,
    scenario: 'public-resume-hydration',
    origin: 'https://localhost:20443',
    errors: { console: 0, externalRequest: 0, page: 0 },
    steps: { published: true, ssr: true, hydrated: true },
  } : mode === 'password-auth' ? {
    ...common,
    scenario: 'password-authentication',
    schemaVersion: 1,
    steps: {
      differentEmailLink: true,
      newPasswordLogin: true,
      oldPasswordRejected: true,
      oldSessionsRevoked: true,
      passwordAdded: true,
      passwordLogin: true,
      providerOnlyLogin: true,
      registerAccepted: true,
      reset: true,
      resetReplayRejected: true,
      verifiedWithoutSession: true,
    },
  } : mode === 'mcp' ? {
    ...common,
    scenario: 'mcp-agent-access',
    schemaVersion: 1,
    steps: {
      clientRegistered: true,
      authorizeRedirected: true,
      consentApproved: true,
      tokenExchanged: true,
      toolsListed: true,
      resumeCreated: true,
      entryUpserted: true,
      editorVisible: true,
      grantRevoked: true,
      revokedRejected: true,
    },
  } : mode === 'privacy' ? {
    ...common,
    scenario: 'account-privacy',
    schemaVersion: 1,
    steps: {
      auth: true,
      export: true,
      cancel: true,
      reauth: true,
      explicitConfirmation: true,
      deletion: true,
      sessionRevoked: true,
      grantRevoked: true,
      publicRevoked: true,
      tombstone: true,
      cleanup: true,
    },
  } : mode === 'exports' ? {
    ...common,
    scenario: 'resume-exports',
    schemaVersion: 1,
    steps: {
      auth: true,
      ownerSaveFirst: true,
      ownerDownload: true,
      ownerPrivacy: true,
      publicPDF: true,
      shareImage: true,
      conditional: true,
      downloadGate: true,
      discoveryIndependent: true,
      revocation: true,
      cleanup: true,
    },
  } : mode === 'publish' ? {
    ...common,
    scenario: 'native-https-publish',
    schemaVersion: 1,
    steps: {
      auth: true,
      complete: true,
      published: true,
      saveFirst: true,
      headers: true,
      noindex: true,
      discovery: true,
      unpublish: true,
      revocation: true,
      cleanup: true,
      signOut: true,
      accessibility: true,
      keyboard: true,
      longInvalidLayout: true,
      publicRealtime: true,
      ownerRealtime: true,
      scroll: true,
    },
    statuses: {
      publish: 200,
      publicPrivate: 200,
      publicDiscoverable: 200,
      unpublish: 200,
      revoked: 404,
    },
    elapsedMs: { revocation: Number.isInteger(actual.elapsedMs?.revocation)
      && actual.elapsedMs.revocation >= 0
      && actual.elapsedMs.revocation <= 5000 ? actual.elapsedMs.revocation : -1 },
    headers: {
      publishContentType: true,
      publishCSRF: true,
      publishIfMatch: true,
      publishIdempotency: true,
      publishSchema: true,
      unpublishContentType: true,
      unpublishCSRF: true,
      unpublishIfMatch: true,
      unpublishIdempotency: true,
      unpublishSchema: true,
    },
  } : mode === 'entry' ? {
    ...common,
    scenario: 'entry-flow',
    schemaVersion: 1,
    steps: {
      landing: true,
      providerLinks: true,
      resumeList: true,
      signIn: true,
      signOut: true,
      signedInShell: true,
    },
  } : mode === 'second-factor-disabled' ? {
    ...common,
    scenario: 'passkey-enrollment-disabled',
    schemaVersion: 1,
    steps: {
      assertionRouteRegistered: true,
      capabilityClosed: true,
      cleanup: true,
      completionNotFound: true,
      enrollmentHidden: true,
      locales: true,
      optionsNotFound: true,
      recoveryRouteRegistered: true,
      removalRouteRegistered: true,
      stateAvailable: true,
      unregisteredRouteMatches: true,
      viewports: true,
    },
  } : mode === 'totp-disabled' ? {
    ...common,
    scenario: 'totp-enrollment-disabled',
    schemaVersion: 1,
    steps: {
      capabilityClosed: true,
      cleanup: true,
      completionNotFound: true,
      enrollmentHidden: true,
      locales: true,
      passkeyStillWorks: true,
      recoveryStillWorks: true,
      removalStillWorks: true,
      startNotFound: true,
      stateAvailable: true,
      unregisteredRouteMatches: true,
      viewports: true,
    },
  } : mode === 'sample-start' ? {
    ...common,
    scenario: 'sample-start',
    schemaVersion: 1,
    steps: {
      capHidesCreate: true,
      created: true,
      editorOpen: true,
      nextAfterVerify: true,
      nextKept: true,
      registered: true,
      reloadCreatedNothing: true,
      signedIn: true,
      verified: true,
    },
  } : {
    schemaVersion: 1,
    scenario: 'authenticated-editor',
    origin: 'https://localhost:20443',
    errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
    steps: {
      auth: true,
      cache: true,
      etag: true,
      ifMatch: true,
      autosave: true,
      conflict: true,
      template: true,
      photo: true,
      session: true,
      persistence: true,
      accessibility: true,
      teardown: true,
    },
  };
  if (JSON.stringify(actual) !== JSON.stringify(expected)) process.exit(1);
}
