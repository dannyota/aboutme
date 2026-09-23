<script setup lang="ts">
import { computed } from 'vue';
import {
  type OAuthConsentDecision,
  type OAuthConsentRequest,
  type OAuthConsentScope,
  OAuthConsentFailure,
  useOAuthConsent,
} from '../composables/useOAuthConsent';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { consentCopy } from '@/i18n/consent';
import { workspaceTitles } from '@/i18n/meta';
import { goToPath } from '@/utils/navigate';

const { locale } = useLocale();
const copy = computed(() => consentCopy[locale.value]);

useHead({ title: computed(() => workspaceTitles[locale.value].authorize) });

const route = useRoute();
const consent = useOAuthConsent();

type PageState = 'loading' | 'ready' | 'invalid' | 'unavailable';

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
  ) {
    return null;
  }

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
  void goToPath(`/login?next=${encodeURIComponent(route.fullPath)}`);
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
    state.value
      = failure instanceof OAuthConsentFailure
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
    state.value
      = failure instanceof OAuthConsentFailure
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
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16"
    data-testid="authorize-page"
  >
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      {{ view ? copy.allowClient(view.clientName) : copy.title }}
    </h1>
    <p
      v-if="view"
      class="mt-4 text-base text-muted-foreground"
    >
      <strong data-testid="consent-client-name">{{ view.clientName }}</strong>
      {{ copy.clientRequest }}
    </p>
    <p
      v-if="state === 'loading'"
      class="mt-8 text-base text-muted-foreground"
    >
      {{ copy.loading }}
    </p>
    <StatusBanner
      v-else-if="state === 'invalid' || state === 'unavailable'"
      ref="errorSummary"
      :focus-on-mount="true"
      class="mt-6"
      kind="error"
      testid="consent-error"
    >
      {{ state === 'invalid' ? copy.invalid : copy.unavailable }}
    </StatusBanner>
    <form
      v-else-if="view"
      class="mt-8 grid gap-6"
      data-testid="consent-form"
      novalidate
      @submit.prevent="submit('approve')"
    >
      <dl
        :aria-label="copy.requestedPermissions"
        class="divide-y border-y"
        data-testid="consent-scopes"
      >
        <template
          v-for="scope in view.scopes"
          :key="scope"
        >
          <dt class="pt-3 font-medium first:pt-4">
            {{ copy.scopes[scope].name }}
          </dt>
          <dd class="pb-3 text-sm text-muted-foreground last:pb-4">
            {{
              copy.scopes[scope].description
            }}
          </dd>
        </template>
      </dl>
      <div class="flex flex-wrap gap-2">
        <Button
          class="h-9"
          data-decision="approve"
          :disabled="pending"
          type="submit"
        >
          {{ pending ? copy.working : copy.approve }}
        </Button>
        <Button
          class="h-9"
          data-decision="deny"
          :disabled="pending"
          type="button"
          variant="ghost"
          @click="submit('deny')"
        >
          {{ copy.deny }}
        </Button>
      </div>
    </form>
  </main>
</template>
