<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue';
import {
  type AgentGrant,
  type AgentGrantScope,
  AgentGrantsFailure,
  useAgentGrants,
} from '../../composables/agentGrants';
import { useAuth } from '../../composables/useAuth';

const { grants, refresh, revoke } = useAgentGrants();
const { csrfToken } = useAuth();

const loading = ref(true);
const unavailable = ref(false);
const selected = ref<AgentGrant | null>(null);
const confirmation = ref<HTMLButtonElement | null>(null);
const returnFocus = ref<HTMLElement | null>(null);
const revokePending = ref(false);

const scopeLabels: Record<AgentGrantScope, string> = {
  'resumes:read': 'Read resumes',
  'resumes:write': 'Write resumes',
};

function formatTime(value: string): string {
  return new Intl.DateTimeFormat('en-US', {
    timeZone: 'UTC',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(new Date(value));
}

function failureKind(error: unknown): AgentGrantsFailure | null {
  return error instanceof AgentGrantsFailure ? error : null;
}

async function load(): Promise<void> {
  loading.value = true;
  unavailable.value = false;
  try {
    await refresh();
  } catch (error) {
    if (failureKind(error)?.kind === 'session-required') {
      await navigateTo('/login');
      return;
    }
    unavailable.value = true;
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  void load();
});

function openConfirmation(grant: AgentGrant, event: MouseEvent): void {
  selected.value = grant;
  returnFocus.value = event.currentTarget instanceof HTMLElement
    ? event.currentTarget
    : null;
  void nextTick(() => confirmation.value?.focus());
}

function closeConfirmation(): void {
  if (revokePending.value) return;
  selected.value = null;
  const target = returnFocus.value;
  returnFocus.value = null;
  void nextTick(() => target?.focus());
}

async function confirmRevoke(): Promise<void> {
  if (revokePending.value || selected.value === null) return;
  revokePending.value = true;
  const grant = selected.value;
  try {
    await revoke(grant.id);
    closeAfterAction();
    await refreshAfterAction();
  } catch (error) {
    const failure = failureKind(error);
    if (failure?.kind === 'session-required') {
      closeAfterAction();
      await navigateTo('/login');
      return;
    }
    if (failure?.kind === 'not-found') {
      closeAfterAction();
      await refreshAfterAction();
      return;
    }
    // Keep the fetched list intact and require a fresh confirmation attempt.
    unavailable.value = true;
    closeAfterAction();
  } finally {
    revokePending.value = false;
  }
}

function closeAfterAction(): void {
  selected.value = null;
  const target = returnFocus.value;
  returnFocus.value = null;
  void nextTick(() => target?.focus());
}

async function refreshAfterAction(): Promise<void> {
  try {
    await refresh();
    unavailable.value = false;
  } catch (error) {
    if (failureKind(error)?.kind === 'session-required') {
      await navigateTo('/login');
      return;
    }
    unavailable.value = true;
  }
}
</script>

<template>
  <section class="connected-agents">
    <h2>Connected agents</h2>

    <p
      v-if="loading"
      data-testid="agents-loading"
    >
      Loading connected agents…
    </p>

    <template v-else>
      <template v-if="unavailable">
        <p
          data-testid="agents-error"
          role="alert"
        >
          Connected agents are unavailable. Try again.
        </p>
        <button
          type="button"
          data-testid="agents-retry"
          @click="load"
        >
          Retry
        </button>
      </template>

      <template v-if="!unavailable && grants.length === 0">
        <p>
          No connected agents. Agents connect through MCP after you approve
          access.
        </p>
      </template>

      <ul v-if="grants.length > 0">
        <li
          v-for="grant in grants"
          :key="grant.id"
          data-testid="agent-row"
        >
          <h3>{{ grant.clientName }}</h3>
          <ul>
            <li
              v-for="scope in grant.scopes"
              :key="scope"
            >
              {{ scopeLabels[scope] }}
            </li>
          </ul>
          <p>
            Created <time :datetime="grant.createdAt">{{
              formatTime(grant.createdAt)
            }}</time>
          </p>
          <p v-if="grant.lastUsedAt !== null">
            Last used <time :datetime="grant.lastUsedAt">{{
              formatTime(grant.lastUsedAt)
            }}</time>
          </p>
          <p v-else>
            Last used Never
          </p>
          <button
            type="button"
            data-testid="agent-revoke"
            :disabled="!csrfToken || revokePending"
            @click="openConfirmation(grant, $event)"
          >
            Revoke
          </button>
        </li>
      </ul>
    </template>

    <div
      v-if="selected !== null"
      role="dialog"
      aria-modal="true"
      aria-labelledby="revoke-agent-title"
      aria-describedby="revoke-agent-description"
      @keydown.esc="closeConfirmation"
    >
      <h3 id="revoke-agent-title">
        Revoke access
      </h3>
      <p id="revoke-agent-description">
        Revoke this connected agent's access?
      </p>
      <form @submit.prevent="confirmRevoke">
        <button
          ref="confirmation"
          type="submit"
          :disabled="revokePending"
        >
          Revoke access
        </button>
        <button
          type="button"
          :disabled="revokePending"
          @click="closeConfirmation"
        >
          Cancel
        </button>
      </form>
    </div>
  </section>
</template>
