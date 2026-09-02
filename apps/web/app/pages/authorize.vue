<script setup lang="ts">
import {
  type OAuthConsentDecision,
  type OAuthConsentRequest,
  type OAuthConsentScope,
  OAuthConsentFailure,
  useOAuthConsent,
} from '../composables/useOAuthConsent';
import AuthCard from '@/components/auth/AuthCard.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

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
  <AuthCard title="Allow access">
    <template #description>
      <template v-if="view">
        <strong data-testid="consent-client-name">{{ view.clientName }}</strong>
        is requesting access to your resumes.
      </template>
    </template>
    <p
      v-if="state === 'loading'"
      class="text-sm text-muted-foreground"
    >
      Loading authorization…
    </p>
    <StatusBanner
      v-else-if="state === 'invalid' || state === 'unavailable'"
      ref="errorSummary"
      :focus-on-mount="true"
      kind="error"
      testid="consent-error"
    >
      {{ state === "invalid" ? INVALID_COPY : UNAVAILABLE_COPY }}
    </StatusBanner>
    <form
      v-else-if="view"
      class="grid gap-4"
      data-testid="consent-form"
      @submit.prevent="submit('approve')"
    >
      <ul
        aria-label="Requested permissions"
        class="flex flex-wrap gap-2"
      >
        <li
          v-for="scope in view.scopes"
          :key="scope"
        >
          <Badge variant="secondary">
            {{ scope === "resumes:read" ? "Read resumes" : "Write resumes" }}
          </Badge>
        </li>
      </ul>
      <div class="flex flex-wrap gap-2">
        <Button
          data-decision="approve"
          :disabled="pending"
          type="submit"
        >
          {{ pending ? "Working…" : "Approve" }}
        </Button>
        <Button
          data-decision="deny"
          :disabled="pending"
          type="button"
          variant="outline"
          @click="submit('deny')"
        >
          Deny
        </Button>
      </div>
    </form>
  </AuthCard>
</template>
