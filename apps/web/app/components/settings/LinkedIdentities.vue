<script setup lang="ts">
/**
 * `LinkedIdentities` — the caller's linked provider identities, each with an
 * Unlink control behind a confirmation.
 *
 * Mirrors the server rule: an identity counts as a sign-in method only while
 * its provider is enabled, so Unlink is disabled when removing an identity
 * would leave no password and no other enabled identity. Everything is keyed
 * by identity id, because one account can hold two identities of the same
 * provider. A stale reauthentication is renewed in place (password) or by a
 * provider round trip, then the unlink is retried.
 */
import ConfirmDialog from '../app/ConfirmDialog.vue';
import StatusBanner from '../app/StatusBanner.vue';
import PasswordField from '../auth/PasswordField.vue';
import { Button } from '../ui/button';
import type { LinkedIdentityActions } from '../../composables/identitySettings';
import type { AuthIdentity, AuthProvider } from '../../composables/useAuth';
import { providerNames } from '../../composables/useCapabilities';

const props = defineProps<{
  identities: readonly AuthIdentity[];
  loginProviders: readonly AuthProvider[];
  hasPassword: boolean;
  actions: LinkedIdentityActions;
}>();
const emit = defineEmits<{ changed: []; unlinked: [name: string] }>();

const lastMethodCopy
  = 'Add a password or link another provider before removing this one.';

type Mode = 'idle' | 'confirm' | 'reauth-password' | 'reauth-provider';

const mode = ref<Mode>('idle');
const target = ref<AuthIdentity | null>(null);
const pending = ref(false);
const errorMessage = ref<string | null>(null);
const currentPassword = ref('');

function isUsable(identity: AuthIdentity): boolean {
  return props.loginProviders.includes(identity.provider);
}

const rows = computed(() =>
  props.identities.map((identity) => {
    const others = props.identities.filter(
      (other) => other.id !== identity.id && isUsable(other),
    );
    return {
      identity,
      name: providerNames[identity.provider],
      usable: isUsable(identity),
      linkedOn: formatDate(identity.createdAt),
      canUnlink: props.hasPassword || others.length > 0,
    };
  }));

// Provider reauthentication needs an enabled, linked provider.
const reauthProviders = computed(() => [
  ...new Set(props.identities.filter(isUsable).map((i) => i.provider)),
]);

const targetName = computed(() =>
  target.value ? providerNames[target.value.provider] : '');
const confirmDescription = computed(() =>
  `You will no longer be able to sign in with this ${targetName.value} `
  + 'account. Your other sign-in methods and signed-in devices stay as they '
  + 'are.');

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat('en-US', {
    timeZone: 'UTC',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(date);
}

function openUnlink(identity: AuthIdentity): void {
  errorMessage.value = null;
  target.value = identity;
  mode.value = 'confirm';
}

function cancel(): void {
  if (pending.value) return;
  mode.value = 'idle';
  target.value = null;
  currentPassword.value = '';
}

function failureKind(error: unknown): unknown {
  return (error as { kind?: unknown } | null)?.kind;
}

async function unlink(): Promise<void> {
  const identity = target.value;
  if (!identity || pending.value) return;
  pending.value = true;
  errorMessage.value = null;
  try {
    await props.actions.unlink(identity.id);
    emit('unlinked', providerNames[identity.provider]);
    finish();
  } catch (error) {
    const kind = failureKind(error);
    if (kind === 'reauth-required') {
      currentPassword.value = '';
      mode.value = props.hasPassword ? 'reauth-password' : 'reauth-provider';
      return;
    }
    if (kind === 'not-found') {
      // Already gone elsewhere; the refreshed list shows the truth.
      finish();
      return;
    }
    mode.value = 'idle';
    errorMessage.value = kind === 'last-method'
      ? lastMethodCopy
      : kind === 'rate-limited'
        ? 'Too many attempts. Try again later.'
        : `Could not unlink ${targetName.value}. Try again.`;
  } finally {
    pending.value = false;
  }
}

function finish(): void {
  mode.value = 'idle';
  target.value = null;
  emit('changed');
}

async function submitPasswordReauth(): Promise<void> {
  if (pending.value) return;
  if (!currentPassword.value) {
    errorMessage.value = 'Enter your current password.';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await props.actions.reauthenticate(currentPassword.value);
  } catch (error) {
    pending.value = false;
    errorMessage.value = failureKind(error) === 'reauth-failed'
      ? 'Incorrect password.'
      : 'Something went wrong. Please try again.';
    return;
  }
  currentPassword.value = '';
  pending.value = false;
  // The confirmation already happened; retry the same unlink.
  await unlink();
}

async function startProviderReauth(provider: AuthProvider): Promise<void> {
  if (pending.value) return;
  pending.value = true;
  errorMessage.value = null;
  try {
    await props.actions.startProviderReauth(provider);
  } catch {
    errorMessage.value = 'Something went wrong. Please try again.';
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <div class="grid gap-4">
    <StatusBanner
      v-if="errorMessage"
      kind="error"
      testid="unlink-error"
      focus-on-mount
    >
      {{ errorMessage }}
    </StatusBanner>
    <ul
      class="divide-y divide-border border-y border-border"
      data-testid="linked-providers"
    >
      <li
        v-for="row in rows"
        :key="row.identity.id"
        class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2
          py-3"
        :data-identity-id="row.identity.id"
        :data-testid="`linked-provider-${row.identity.provider}`"
      >
        <div class="grid gap-0.5">
          <span class="font-medium">{{ row.name }}</span>
          <span class="text-sm text-muted-foreground">
            {{ row.linkedOn ? `Linked on ${row.linkedOn}` : 'Linked' }}{{
              row.usable ? '' : '. Not available for sign-in'
            }}
          </span>
          <span
            v-if="!row.canUnlink"
            :id="`unlink-blocked-${row.identity.id}`"
            class="text-sm text-muted-foreground"
            data-testid="unlink-blocked"
          >
            {{ lastMethodCopy }}
          </span>
        </div>
        <Button
          :aria-describedby="
            row.canUnlink ? undefined : `unlink-blocked-${row.identity.id}`
          "
          data-testid="unlink-button"
          :disabled="!row.canUnlink || pending"
          size="sm"
          type="button"
          variant="outline"
          @click="openUnlink(row.identity)"
        >
          Unlink
        </Button>
      </li>
    </ul>

    <form
      v-if="mode === 'reauth-password'"
      class="grid gap-4"
      data-testid="unlink-reauth-password"
      novalidate
      @submit.prevent="submitPasswordReauth"
    >
      <p>
        Sign in again to confirm it’s you before unlinking {{ targetName }}.
      </p>
      <PasswordField
        id="unlink-current-password"
        v-model="currentPassword"
        autocomplete="current-password"
        label="Current password"
      />
      <div class="flex gap-2">
        <Button
          :disabled="pending"
          type="submit"
          variant="secondary"
        >
          {{ pending ? 'Checking…' : 'Continue' }}
        </Button>
        <Button
          :disabled="pending"
          type="button"
          variant="ghost"
          @click="cancel"
        >
          Cancel
        </Button>
      </div>
    </form>

    <div
      v-else-if="mode === 'reauth-provider'"
      class="grid gap-3"
      data-testid="unlink-reauth-provider"
    >
      <p>
        Sign in again with your provider, then unlink {{ targetName }} again.
      </p>
      <div class="flex flex-wrap gap-2">
        <Button
          v-for="provider in reauthProviders"
          :key="provider"
          :disabled="pending"
          type="button"
          @click="startProviderReauth(provider)"
        >
          Continue with {{ providerNames[provider] }}
        </Button>
        <Button
          :disabled="pending"
          type="button"
          variant="ghost"
          @click="cancel"
        >
          Cancel
        </Button>
      </div>
    </div>

    <ConfirmDialog
      :busy="pending"
      cancel-action="cancel-unlink"
      class="max-w-md"
      confirm-action="confirm-unlink"
      :confirm-label="`Unlink ${targetName}`"
      :description="confirmDescription"
      destructive
      :open="mode === 'confirm'"
      :title="`Unlink ${targetName}?`"
      @cancel="cancel"
      @confirm="unlink"
    />
  </div>
</template>
