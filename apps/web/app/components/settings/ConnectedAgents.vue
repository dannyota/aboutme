<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import ConfirmDialog from '../app/ConfirmDialog.vue';
import EmptyState from '../app/EmptyState.vue';
import LoadingState from '../app/LoadingState.vue';
import StatusBanner from '../app/StatusBanner.vue';
import LocaleToggle from '../app/LocaleToggle.vue';
import { Badge } from '../ui/badge';
import { Button } from '../ui/button';
import {
  type AgentGrant,
  AgentGrantsFailure,
  useAgentGrants,
} from '../../composables/agentGrants';
import { useAuth } from '../../composables/useAuth';
import { agentSettingsCopy } from '../../i18n/agent-settings';
import { goToPath } from '@/utils/navigate';

const { grants, refresh, revoke } = useAgentGrants();
const { csrfToken } = useAuth();

const loading = ref(true);
const unavailable = ref(false);
const selected = ref<AgentGrant | null>(null);
const revokePending = ref(false);
const { locale } = useLocale();
const copy = computed(() => agentSettingsCopy[locale.value]);

function formatTime(value: string): string {
  return new Intl.DateTimeFormat(locale.value === 'vi' ? 'vi-VN' : 'en-US', {
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
      await goToPath('/login');
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
      await goToPath('/login');
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
      await goToPath('/login');
      return;
    }
    unavailable.value = true;
  }
}
</script>

<template>
  <div
    data-testid="connected-agents"
    class="grid gap-4"
  >
    <h2
      id="agents-title"
      class="text-lg font-semibold"
    >
      {{ copy.title }}
    </h2>
    <LoadingState
      v-if="loading"
      :label="copy.loading"
      testid="agents-loading"
    />

    <template v-else>
      <template v-if="unavailable">
        <StatusBanner
          kind="error"
          testid="agents-error"
        >
          {{ copy.unavailable }}
        </StatusBanner>
        <Button
          data-testid="agents-retry"
          type="button"
          variant="outline"
          @click="load"
        >
          {{ copy.retry }}
        </Button>
      </template>

      <EmptyState
        v-if="!unavailable && grants.length === 0"
        :title="copy.emptyTitle"
        :description="copy.emptyDescription"
      />

      <div
        v-if="grants.length > 0"
        class="grid gap-3"
      >
        <div
          v-for="grant in grants"
          :key="grant.id"
          data-testid="agent-row"
          class="grid gap-3 border-b py-4 last:border-b-0"
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
              {{ copy.scopes[scope] }}
            </Badge>
          </div>
          <p class="text-muted-foreground text-sm">
            {{ copy.created }}
            <time :datetime="grant.createdAt">{{
              formatTime(grant.createdAt)
            }}</time>
          </p>
          <p class="text-muted-foreground text-sm">
            <time
              v-if="grant.lastUsedAt !== null"
              :datetime="grant.lastUsedAt"
            >{{ copy.lastUsed }} {{ formatTime(grant.lastUsedAt) }}</time>
            <span v-else>{{ copy.neverUsed }}</span>
          </p>
          <Button
            data-testid="agent-revoke"
            :disabled="!csrfToken || revokePending"
            type="button"
            variant="secondary"
            @click="openConfirmation(grant)"
          >
            {{ copy.revoke }}
          </Button>
        </div>
      </div>
    </template>

    <ConfirmDialog
      :open="selected !== null"
      :title="copy.revokeTitle"
      :description="copy.revokeDescription"
      :confirm-label="copy.revokeConfirm"
      :cancel-label="copy.cancel"
      destructive
      :busy="revokePending"
      confirm-action="agent-revoke-confirm"
      cancel-action="agent-revoke-cancel"
      @confirm="confirmRevoke"
      @cancel="closeConfirmation"
    >
      <template #header-actions>
        <LocaleToggle
          :label="copy.localeLabel"
          @pointerdown.prevent
        />
      </template>
    </ConfirmDialog>
  </div>
</template>
