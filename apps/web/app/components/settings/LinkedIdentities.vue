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
import LocaleToggle from '../app/LocaleToggle.vue';
import StatusBanner from '../app/StatusBanner.vue';
import ReauthPrompt from './ReauthPrompt.vue';
import { Button } from '../ui/button';
import type { LinkedIdentityActions } from '../../composables/identitySettings';
import type { AuthIdentity, AuthProvider } from '../../composables/useAuth';
import { providerNames } from '../../composables/useCapabilities';
import { identitySettingsCopy } from '../../i18n/identity-settings';
import { shellCopy } from '../../i18n/shell';

const props = defineProps<{
  identities: readonly AuthIdentity[];
  loginProviders: readonly AuthProvider[];
  hasPassword: boolean;
  actions: LinkedIdentityActions;
}>();
const emit = defineEmits<{ changed: []; unlinked: [name: string] }>();

type Mode = 'idle' | 'confirm' | 'reauth-password' | 'reauth-provider';
type ErrorKind
  = | 'current-password-required'
    | 'reauth-failed'
    | 'last-method'
    | 'rate-limited'
    | 'unavailable';

const mode = ref<Mode>('idle');
const target = ref<AuthIdentity | null>(null);
const pending = ref(false);
const errorKind = ref<ErrorKind | null>(null);
const currentPassword = ref('');
const locale = useRouteLocale();
const copy = computed(() => identitySettingsCopy[locale.value]);
const errorMessage = computed(() => {
  if (errorKind.value === null) return null;
  const errors: Record<Exclude<ErrorKind, null>, string> = {
    'current-password-required': copy.value.errors.currentPasswordRequired,
    'reauth-failed': copy.value.errors.reauthFailed,
    'last-method': copy.value.errors.lastMethod,
    'rate-limited': copy.value.errors.rateLimited,
    'unavailable': copy.value.errors.unavailable,
  };
  return errors[errorKind.value];
});

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
  copy.value.unlinkDescription(targetName.value));

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(locale.value === 'vi' ? 'vi-VN' : 'en-US', {
    timeZone: 'UTC',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(date);
}

function openUnlink(identity: AuthIdentity): void {
  errorKind.value = null;
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
  errorKind.value = null;
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
    errorKind.value = kind === 'last-method'
      ? 'last-method'
      : kind === 'rate-limited'
        ? 'rate-limited'
        : 'unavailable';
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
    errorKind.value = 'current-password-required';
    return;
  }
  pending.value = true;
  errorKind.value = null;
  try {
    await props.actions.reauthenticate(currentPassword.value);
  } catch (error) {
    pending.value = false;
    errorKind.value = failureKind(error) === 'reauth-failed'
      ? 'reauth-failed'
      : 'unavailable';
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
  errorKind.value = null;
  try {
    await props.actions.startProviderReauth(provider);
  } catch {
    errorKind.value = 'unavailable';
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
            {{ row.linkedOn ? copy.linkedOn(row.linkedOn) : copy.linked }}{{
              row.usable ? '' : copy.unavailableForSignIn
            }}
          </span>
          <span
            v-if="!row.canUnlink"
            :id="`unlink-blocked-${row.identity.id}`"
            class="text-sm text-muted-foreground"
            data-testid="unlink-blocked"
          >
            {{ copy.lastMethod }}
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
          {{ copy.unlink }}
        </Button>
      </li>
    </ul>

    <ReauthPrompt
      v-if="mode === 'reauth-password' || mode === 'reauth-provider'"
      v-model="currentPassword"
      :mode="mode === 'reauth-password' ? 'password' : 'provider'"
      :providers="reauthProviders"
      :provider-label="(provider) => copy.continueWith(providerNames[provider])"
      :pending="pending"
      :locale="locale"
      password-id="unlink-current-password"
      :labels="copy"
      :password-description="copy.passwordReauthDescription(targetName)"
      :provider-description="copy.providerReauthDescription(targetName)"
      lock-cancel
      form-testid="unlink-reauth-password"
      provider-testid="unlink-reauth-provider"
      @submit-password="submitPasswordReauth"
      @provider="startProviderReauth"
      @cancel="cancel"
    />

    <ConfirmDialog
      :busy="pending"
      cancel-action="cancel-unlink"
      :cancel-label="copy.cancel"
      class="max-w-md"
      confirm-action="confirm-unlink"
      :confirm-label="copy.unlinkConfirm(targetName)"
      :description="confirmDescription"
      destructive
      :open="mode === 'confirm'"
      :title="copy.unlinkTitle(targetName)"
      @cancel="cancel"
      @confirm="unlink"
    >
      <template #header-actions>
        <LocaleToggle
          :label="shellCopy[locale].localeLabel"
          @pointerdown.prevent
        />
      </template>
    </ConfirmDialog>
  </div>
</template>
