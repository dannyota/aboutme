import { ref, type ComputedRef, type Ref } from 'vue';

import type { ResumeMutationCoordinator } from './coordinator';
import type { useResumeStore } from '../stores/resumes';
import type { useAuth, AuthProvider } from '../composables/useAuth';
import {
  createPublishApi,
  freezePublishAttempt,
  type FrozenPublishAttempt,
  type PublishApi,
  type PublishCommand,
  type PublishFailureCode,
  type PublishResult,
} from './publishApi';
import { validateAuthorizeUrl } from '../composables/providerAuthorization';
import {
  mapReauthError,
  mapReauthStartError,
} from '../composables/passwordSettings';
import type { EditorRuntime } from './types';

export type PublishControllerState
  = | { readonly kind: 'idle' }
    | { readonly kind: 'saving' }
    | { readonly kind: 'dispatching'; readonly attempt: FrozenPublishAttempt }
    | {
      readonly kind: 'blocked';
      readonly reason:
        | 'not-loaded'
        | 'saving'
        | 'conflict'
        | 'session-lost'
        | 'issue'
        | 'partial-template'
        | 'opaque-photo'
        | 'read-required';
    }
    | Extract<
      PublishResult,
      {
        kind:
          | 'accepted'
          | 'invalid'
          | 'slug-taken'
          | 'stale'
          | 'rate-limited'
          | 'public-state-busy'
          | 'session-lost'
          | 'unknown';
      }
    >
    | {
      readonly kind: 'failed';
      readonly code:
        | PublishFailureCode
        | 'csrf_rejected'
        | 'provider_disabled'
        | 'provider_unavailable'
        | 'response_invalid'
        | 'save_failed';
    }
    | {
      readonly kind: 'reauth-required';
      readonly method: 'password' | 'provider';
      readonly attempt: FrozenPublishAttempt;
    }
    | {
      readonly kind: 'reauth-wrong-password';
      readonly method: 'password';
      readonly attempt: FrozenPublishAttempt;
    }
    | {
      readonly kind: 'reauth-rate-limited';
      readonly method: 'password' | 'provider';
      readonly attempt: FrozenPublishAttempt;
    }
    | {
      readonly kind: 'reauth-unavailable';
      readonly method: 'password' | 'provider';
      readonly attempt: FrozenPublishAttempt;
    }
    | {
      readonly kind: 'provider-started-rate-limited';
      readonly method: 'provider';
      readonly attempt: FrozenPublishAttempt;
    }
    | {
      readonly kind: 'provider-start-invalid';
      readonly method: 'provider';
      readonly attempt: FrozenPublishAttempt;
    }
    | {
      readonly kind: 'provider-started';
      readonly authorizeUrl: string;
      readonly attempt: FrozenPublishAttempt;
    };

export interface PublishController {
  readonly state: Readonly<Ref<PublishControllerState>>;
  submit(command: PublishCommand): Promise<PublishControllerState>;
  retryUncertain(): Promise<PublishControllerState>;
  reauthPassword(password: string): Promise<PublishControllerState>;
  startProviderReauth(): Promise<PublishControllerState>;
  retryAfterProviderReauth(): Promise<PublishControllerState>;
  cancel(): void;
}

export interface PublishControllerDeps {
  readonly resumeId: string;
  readonly store: ReturnType<typeof useResumeStore>;
  readonly coordinator: Pick<
    ResumeMutationCoordinator,
    'flush' | 'completeRead'
  >;
  readonly auth: Pick<
    ReturnType<typeof useAuth>,
    'user' | 'csrfToken' | 'authState' | 'identities' | 'refresh' | 'mutate'
  >;
  readonly api?: PublishApi;
  readonly runtime: Pick<EditorRuntime, 'nowEpochMs' | 'uuid' | 'delay'>;
  readonly providerLogin: ComputedRef<boolean> | boolean;
}

export function createPublishController(
  deps: PublishControllerDeps,
): PublishController {
  const state = ref<PublishControllerState>({ kind: 'idle' });
  const api = deps.api ?? createPublishApi();
  let attempt: FrozenPublishAttempt | null = null;
  let csrfRetried = false;
  let submitting = false;
  let retryBusy = false;
  let passwordBusy = false;
  let providerBusy = false;
  let generation = 0;

  const providerEnabled = (): boolean =>
    typeof deps.providerLogin === 'boolean'
      ? deps.providerLogin
      : deps.providerLogin.value;

  async function dispatchCurrent(
    runGeneration = generation,
  ): Promise<PublishControllerState> {
    if (runGeneration !== generation) return state.value;
    if (attempt === null) {
      return setState({ kind: 'blocked', reason: 'not-loaded' });
    }
    if (!sameOwner(attempt, deps.auth)) {
      return loseSession();
    }
    const csrf = deps.auth.csrfToken.value;
    if (csrf === null || deps.auth.authState.value !== 'authenticated') {
      return loseSession();
    }
    setState({ kind: 'dispatching', attempt });
    const result = await api.dispatch(attempt, csrf);
    if (runGeneration !== generation || attempt === null) return state.value;
    if (!sameOwner(attempt, deps.auth)) {
      return loseSession();
    }
    if (result.kind === 'csrf-rejected' && !csrfRetried) {
      csrfRetried = true;
      try {
        await deps.auth.refresh();
      } catch {
        if (runGeneration !== generation || attempt === null) {
          return state.value;
        }
        return setState({ kind: 'failed', code: 'csrf_rejected' });
      }
      return dispatchCurrent(runGeneration);
    }
    if (result.kind === 'csrf-rejected') {
      return setState({ kind: 'failed', code: 'csrf_rejected' });
    }
    if (result.kind === 'accepted') {
      if (!sameOwner(attempt, deps.auth)) {
        attempt = null;
        return setState({ kind: 'session-lost' });
      }
      deps.store.adoptComplete(deps.resumeId, result.resume);
      attempt = null;
      return setState(result);
    }
    if (result.kind === 'stale') {
      const staleAttempt = attempt;
      const metadata = deps.store.recordFor(deps.resumeId)?.accepted.metadata;
      if (metadata === undefined) {
        return setState({ kind: 'blocked', reason: 'not-loaded' });
      }
      deps.store.adoptStaleWinner(deps.resumeId, {
        ...result.winner,
        metadata,
        metadataFreshness: 'stale',
      });
      attempt = null;
      const readComplete = await deps.coordinator
        .completeRead(deps.resumeId)
        .catch(() => false);
      if (runGeneration !== generation) return state.value;
      if (!sameOwner(staleAttempt, deps.auth)) return loseSession();
      const refreshed = deps.store.recordFor(deps.resumeId);
      if (
        !readComplete
        || refreshed === undefined
        || refreshed.completeReadRequired
        || refreshed.accepted.metadataFreshness !== 'complete'
      ) {
        return setState({ kind: 'blocked', reason: 'read-required' });
      }
      return setState(result);
    }
    if (result.kind === 'reauth-required') {
      const hasPassword = deps.auth.user.value?.hasPassword !== false;
      if (!hasPassword && !providerEnabled()) {
        return setState({ kind: 'failed', code: 'provider_disabled' });
      }
      if (!hasPassword && stableProvider(deps.auth) === null) {
        return setState({ kind: 'failed', code: 'provider_unavailable' });
      }
      const method = hasPassword ? 'password' : 'provider';
      return setState({ kind: 'reauth-required', method, attempt });
    }
    if (result.kind === 'unknown') {
      return setState(result);
    }
    if (result.kind === 'session-lost') {
      return loseSession();
    }
    return setState(result);
  }

  async function submit(
    command: PublishCommand,
  ): Promise<PublishControllerState> {
    if (
      submitting
      || state.value.kind === 'dispatching'
      || state.value.kind === 'saving'
    ) {
      return state.value;
    }
    submitting = true;
    const runGeneration = generation;
    setState({ kind: 'saving' });
    try {
      await deps.coordinator.flush(deps.resumeId);
      if (runGeneration !== generation) return state.value;
      const record = deps.store.recordFor(deps.resumeId);
      if (record === undefined) {
        return setState({ kind: 'blocked', reason: 'not-loaded' });
      }
      const blocked = blockedReason(record);
      if (blocked !== null) {
        return setState({ kind: 'blocked', reason: blocked });
      }
      const commandIssue = validateCommand(command);
      if (commandIssue !== null) {
        return setState({ kind: 'invalid', issues: [commandIssue] });
      }
      if (deps.auth.authState.value !== 'authenticated') {
        return loseSession();
      }
      const ownerId = deps.auth.user.value?.id;
      if (ownerId === undefined) return loseSession();
      attempt = freezePublishAttempt(
        deps.resumeId,
        record.accepted.revision,
        command,
        deps.runtime,
        ownerId,
      );
      csrfRetried = false;
      return await dispatchCurrent(runGeneration);
    } catch {
      return runGeneration === generation
        ? setState({ kind: 'failed', code: 'save_failed' })
        : state.value;
    } finally {
      submitting = false;
    }
  }

  async function retryUncertain(): Promise<PublishControllerState> {
    if (retryBusy || state.value.kind !== 'unknown' || attempt === null) {
      return state.value;
    }
    retryBusy = true;
    try {
      return await dispatchCurrent();
    } finally {
      retryBusy = false;
    }
  }

  async function reauthPassword(
    password: string,
  ): Promise<PublishControllerState> {
    if (
      passwordBusy
      || attempt === null
      || !(
        state.value.kind === 'reauth-required'
        || state.value.kind === 'reauth-wrong-password'
        || (state.value.kind === 'reauth-rate-limited'
          && state.value.method === 'password')
        || (state.value.kind === 'reauth-unavailable'
          && state.value.method === 'password')
      )
    ) { return state.value; }
    const runGeneration = generation;
    const frozen = attempt;
    passwordBusy = true;
    try {
      if (!sameOwner(frozen, deps.auth)) {
        return loseSession();
      }
      await deps.auth.mutate('/api/v1/auth/password/reauth', {
        method: 'POST',
        body: { password },
      });
    } catch (error) {
      if (
        runGeneration !== generation
        || attempt === null
        || attempt !== frozen
      ) {
        return state.value;
      }
      const failure = mapReauthError(error);
      if (failure.kind === 'reauth-failed') {
        return setState({
          kind: 'reauth-wrong-password',
          method: 'password',
          attempt: frozen,
        });
      }
      if (failure.kind === 'rate-limited') {
        return setState({
          kind: 'reauth-rate-limited',
          method: 'password',
          attempt: frozen,
        });
      }
      return setState({
        kind: 'reauth-unavailable',
        method: 'password',
        attempt: frozen,
      });
    } finally {
      passwordBusy = false;
    }
    if (
      runGeneration !== generation
      || attempt === null
      || attempt !== frozen
    ) {
      return state.value;
    }
    if (!sameOwner(frozen, deps.auth)) return loseSession();
    csrfRetried = false;
    return dispatchCurrent(runGeneration);
  }

  async function startProviderReauth(): Promise<PublishControllerState> {
    if (providerBusy || attempt === null || !providerStartState(state.value)) {
      return state.value;
    }
    providerBusy = true;
    const runGeneration = generation;
    const frozen = attempt;
    try {
      if (!sameOwner(frozen, deps.auth)) {
        return loseSession();
      }
      if (!providerEnabled()) {
        return setState({ kind: 'failed', code: 'provider_disabled' });
      }
      const provider = stableProvider(deps.auth);
      if (provider === null) {
        return setState({ kind: 'failed', code: 'provider_unavailable' });
      }
      const response = await deps.auth.mutate<{
        data?: { authorizeUrl?: unknown };
      }>(`/api/v1/auth/${provider}/start`, {
        method: 'POST',
        query: { purpose: 'reauth' },
      });
      if (
        runGeneration !== generation
        || attempt === null
        || attempt !== frozen
      ) {
        return state.value;
      }
      const authorizeUrl = validateAuthorizeUrl(
        provider,
        response?.data?.authorizeUrl,
      );
      if (authorizeUrl === null) {
        return setState({
          kind: 'provider-start-invalid',
          method: 'provider',
          attempt: frozen,
        });
      }
      if (runGeneration !== generation) return state.value;
      if (!sameOwner(frozen, deps.auth)) {
        return loseSession();
      }
      return setState({
        kind: 'provider-started',
        authorizeUrl,
        attempt: frozen,
      });
    } catch (error) {
      if (
        runGeneration !== generation
        || attempt === null
        || attempt !== frozen
      ) {
        return state.value;
      }
      if (mapReauthStartError(error).kind === 'rate-limited') {
        return setState({
          kind: 'provider-started-rate-limited',
          method: 'provider',
          attempt: frozen,
        });
      }
      return setState({
        kind: 'reauth-unavailable',
        method: 'provider',
        attempt: frozen,
      });
    } finally {
      providerBusy = false;
    }
  }

  async function retryAfterProviderReauth(): Promise<PublishControllerState> {
    if (
      retryBusy
      || attempt === null
      || state.value.kind !== 'provider-started'
    ) {
      return state.value;
    }
    retryBusy = true;
    const runGeneration = generation;
    const frozen = attempt;
    try {
      if (!sameOwner(frozen, deps.auth)) {
        return loseSession();
      }
      await deps.auth.refresh();
      if (
        runGeneration !== generation
        || attempt === null
        || attempt !== frozen
      ) {
        return state.value;
      }
      if (!sameOwner(frozen, deps.auth)) {
        return loseSession();
      }
      csrfRetried = false;
      return await dispatchCurrent(runGeneration);
    } catch {
      return runGeneration === generation && attempt === frozen
        ? loseSession()
        : state.value;
    } finally {
      retryBusy = false;
    }
  }

  function cancel(): void {
    if (
      submitting
      || retryBusy
      || passwordBusy
      || providerBusy
      || state.value.kind === 'saving'
      || state.value.kind === 'dispatching'
    ) {
      return;
    }
    generation += 1;
    attempt = null;
    csrfRetried = false;
    setState({ kind: 'idle' });
  }

  function setState(next: PublishControllerState): PublishControllerState {
    state.value = next;
    return next;
  }

  function loseSession(): PublishControllerState {
    attempt = null;
    deps.store.markSessionLost(deps.resumeId);
    return setState({ kind: 'session-lost' });
  }

  return {
    state,
    submit,
    retryUncertain,
    reauthPassword,
    startProviderReauth,
    retryAfterProviderReauth,
    cancel,
  };
}

function blockedReason(record: {
  pending: readonly unknown[];
  attempt: unknown;
  conflicts: readonly unknown[];
  sessionLost: boolean;
  issues: Readonly<Record<string, readonly unknown[]>>;
  templateState: { kind: string } | null;
  opaquePhotoOutcome: unknown;
  completeReadRequired: boolean;
}): Extract<PublishControllerState, { kind: 'blocked' }>['reason'] | null {
  if (record.sessionLost) return 'session-lost';
  if (record.conflicts.length > 0) return 'conflict';
  if (record.templateState?.kind === 'partial') return 'partial-template';
  if (record.opaquePhotoOutcome !== null) return 'opaque-photo';
  if (record.completeReadRequired) return 'read-required';
  if (record.pending.length > 0 || record.attempt !== null) return 'saving';
  if (Object.keys(record.issues).length > 0) return 'issue';
  return null;
}

function validateCommand(
  command: PublishCommand,
): { readonly path: string; readonly code: 'invalid_format' } | null {
  if (
    command.slug !== undefined
    && (command.slug.length < 4
      || command.slug.length > 30
      || !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(command.slug))
  ) {
    return { path: 'slug', code: 'invalid_format' };
  }
  return null;
}

function stableProvider(
  auth: Pick<ReturnType<typeof useAuth>, 'identities'>,
): AuthProvider | null {
  return auth.identities.value[0]?.provider ?? null;
}

function providerStartState(state: PublishControllerState): boolean {
  return (
    (state.kind === 'reauth-required'
      || state.kind === 'reauth-unavailable'
      || state.kind === 'reauth-rate-limited'
      || state.kind === 'provider-start-invalid'
      || state.kind === 'provider-started-rate-limited')
    && state.method === 'provider'
  );
}

function sameOwner(
  attempt: FrozenPublishAttempt,
  auth: Pick<ReturnType<typeof useAuth>, 'user' | 'authState'>,
): boolean {
  return (
    auth.authState.value === 'authenticated'
    && auth.user.value?.id === attempt.ownerId
  );
}
