<script setup lang="ts">
import { onMounted, ref } from 'vue';
import ConfirmDialog from '../app/ConfirmDialog.vue';
import EmptyState from '../app/EmptyState.vue';
import LoadingState from '../app/LoadingState.vue';
import StatusBanner from '../app/StatusBanner.vue';
import { Badge } from '../ui/badge';
import { Button } from '../ui/button';
import { Card, CardContent, CardHeader } from '../ui/card';
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

function openConfirmation(grant: AgentGrant): void {
  selected.value = grant;
}

function closeConfirmation(): void {
  if (revokePending.value) return;
  closeAfterAction();
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
  <Card data-testid="connected-agents">
    <CardHeader>
      <h2 class="leading-none font-semibold">
        Connected agents
      </h2>
    </CardHeader>
    <CardContent class="grid gap-4">
      <LoadingState
        v-if="loading"
        label="Loading connected agents…"
        testid="agents-loading"
      />

      <template v-else>
        <template v-if="unavailable">
          <StatusBanner
            kind="error"
            testid="agents-error"
          >
            Connected agents are unavailable. Try again.
          </StatusBanner>
          <Button
            data-testid="agents-retry"
            type="button"
            variant="outline"
            @click="load"
          >
            Retry
          </Button>
        </template>

        <EmptyState
          v-if="!unavailable && grants.length === 0"
          title="No connected agents."
          description="Agents connect through MCP after you approve access."
        />

        <div
          v-if="grants.length > 0"
          class="grid gap-3"
        >
          <div
            v-for="grant in grants"
            :key="grant.id"
            data-testid="agent-row"
            class="grid gap-3 rounded-lg border p-4"
          >
            <h3 class="font-medium">
              {{ grant.clientName }}
            </h3>
            <div class="flex flex-wrap gap-2">
              <Badge
                v-for="scope in grant.scopes"
                :key="scope"
                variant="secondary"
              >
                {{ scopeLabels[scope] }}
              </Badge>
            </div>
            <p class="text-muted-foreground text-sm">
              Created
              <time :datetime="grant.createdAt">{{
                formatTime(grant.createdAt)
              }}</time>
            </p>
            <p class="text-muted-foreground text-sm">
              Last used
              <time
                v-if="grant.lastUsedAt !== null"
                :datetime="grant.lastUsedAt"
              >{{ formatTime(grant.lastUsedAt) }}</time>
              <span v-else>Never</span>
            </p>
            <Button
              data-testid="agent-revoke"
              :disabled="!csrfToken || revokePending"
              type="button"
              variant="outline"
              @click="openConfirmation(grant)"
            >
              Revoke
            </Button>
          </div>
        </div>
      </template>
    </CardContent>
    <ConfirmDialog
      :open="selected !== null"
      title="Revoke access"
      description="Revoke this connected agent's access?"
      confirm-label="Revoke access"
      destructive
      :busy="revokePending"
      confirm-action="agent-revoke-confirm"
      cancel-action="agent-revoke-cancel"
      @confirm="confirmRevoke"
      @cancel="closeConfirmation"
    />
  </Card>
</template>
