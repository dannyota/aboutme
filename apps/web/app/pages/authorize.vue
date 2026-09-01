<script setup lang="ts">
import '~/assets/css/auth.css';
import {
  type OAuthConsentDecision,
  type OAuthConsentRequest,
  type OAuthConsentScope,
  OAuthConsentFailure,
  useOAuthConsent,
} from '../composables/useOAuthConsent';

const route = useRoute();
const consent = useOAuthConsent();

type PageState = 'loading' | 'ready' | 'invalid' | 'unavailable';

const INVALID_COPY = 'This authorization request is invalid.';
const UNAVAILABLE_COPY = 'Unable to load authorization. Please try again.';
const state = ref<PageState>('loading');
const view = ref<{
  clientName: string;
  scopes: OAuthConsentScope[];
} | null>(null);
const errorSummary = ref<HTMLElement | null>(null);
const pending = ref(false);

function queryString(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null;
}

function parseQuery(): OAuthConsentRequest | null {
  const values = route.query;
  const clientId = queryString(values.client_id);
  const redirectURI = queryString(values.redirect_uri);
  const responseType = queryString(values.response_type);
  const scope = queryString(values.scope);
  const codeChallenge = queryString(values.code_challenge);
  const codeChallengeMethod = queryString(values.code_challenge_method);
  const stateValue = values.state;
  if (
    clientId === null
    || redirectURI === null
    || responseType !== 'code'
    || (scope !== 'resumes:read'
      && scope !== 'resumes:write'
      && scope !== 'resumes:read resumes:write')
    || codeChallenge === null
    || codeChallengeMethod !== 'S256'
    || (stateValue !== undefined && typeof stateValue !== 'string')
  ) return null;

  return {
    client_id: clientId,
    redirect_uri: redirectURI,
    response_type: 'code',
    scope,
    ...(stateValue === undefined ? {} : { state: stateValue }),
    code_challenge: codeChallenge,
    code_challenge_method: 'S256',
  };
}

const authorizeQuery = parseQuery();

function loginForSession(): void {
  void navigateTo(`/login?next=${encodeURIComponent(route.fullPath)}`);
}

async function focusError(): Promise<void> {
  await nextTick();
  errorSummary.value?.focus();
}

async function load(): Promise<void> {
  if (authorizeQuery === null) {
    state.value = 'invalid';
    await focusError();
    return;
  }
  try {
    view.value = await consent.get(authorizeQuery);
    state.value = 'ready';
  } catch (failure) {
    if (
      failure instanceof OAuthConsentFailure
      && failure.kind === 'session-required'
    ) {
      loginForSession();
      return;
    }
    state.value = failure instanceof OAuthConsentFailure
      && failure.kind === 'invalid-request'
      ? 'invalid'
      : 'unavailable';
    await focusError();
  }
}

async function submit(decision: OAuthConsentDecision): Promise<void> {
  if (pending.value || authorizeQuery === null) return;
  pending.value = true;
  state.value = 'ready';
  try {
    const result = await consent.decide(authorizeQuery, decision);
    await navigateTo(result.redirectTo, { external: true });
  } catch (failure) {
    if (
      failure instanceof OAuthConsentFailure
      && failure.kind === 'session-required'
    ) {
      loginForSession();
      return;
    }
    state.value = failure instanceof OAuthConsentFailure
      && failure.kind === 'invalid-request'
      ? 'invalid'
      : 'unavailable';
    await focusError();
  } finally {
    pending.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <main class="aboutme-app">
    <section class="app-page app-page--narrow">
      <div class="login">
        <h1>Connect an agent</h1>

        <p
          v-if="state === 'loading'"
          class="auth-note"
        >
          Loading authorization…
        </p>

        <div
          v-else-if="state === 'invalid' || state === 'unavailable'"
          ref="errorSummary"
          data-testid="consent-error"
          class="auth-error-summary"
          role="alert"
          tabindex="-1"
        >
          {{ state === 'invalid' ? INVALID_COPY : UNAVAILABLE_COPY }}
        </div>

        <form
          v-else-if="view"
          class="auth-form"
          @submit.prevent="submit('approve')"
        >
          <p>
            <span data-testid="consent-client-name">{{ view.clientName }}</span>
            is requesting access to your resumes.
          </p>

          <ul aria-label="Requested permissions">
            <li
              v-for="scope in view.scopes"
              :key="scope"
            >
              {{ scope === 'resumes:read' ? 'Read resumes' : 'Write resumes' }}
            </li>
          </ul>

          <button
            type="submit"
            class="auth-submit"
            data-decision="approve"
            :disabled="pending"
          >
            {{ pending ? 'Working…' : 'Approve' }}
          </button>
          <button
            type="button"
            data-decision="deny"
            :disabled="pending"
            @click="submit('deny')"
          >
            Deny
          </button>
        </form>
      </div>
    </section>
  </main>
</template>
