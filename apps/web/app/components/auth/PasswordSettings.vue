<script setup lang="ts">
/**
 * `PasswordSettings`: add or change a password from account settings.
 *
 * Presentational: it receives the current `hasPassword` status and the
 * linked providers as props, and it performs exactly three side effects
 * through `PasswordSettingsActionsKey` (see `composables/passwordSettings`).
 * It never reads ambient auth state, and it never renders a provider email.
 *
 * Provider-only users add a password optimistically and fall back to a
 * provider reauth round trip only when the server answers `reauth_required`.
 * Password users reauthenticate with their current password before entering
 * a new one. The confirmation field is component-local (via `PasswordField`)
 * and is never submitted; `setPassword` receives exactly one password.
 */
import PasswordField from './PasswordField.vue';
import StatusBanner from '../app/StatusBanner.vue';
import { Button } from '../ui/button';
import {
  PasswordSettingsActionsKey,
  PasswordSettingsFailure,
} from '../../composables/passwordSettings';
import type { AuthProvider } from '../../composables/useAuth';
import { providerNames } from '@/composables/useCapabilities';
import {
  passwordSettingsCopy,
  type PasswordSettingsMessage,
} from '@/i18n/password-settings';

const props = defineProps<{
  hasPassword: boolean;
  providers: AuthProvider[];
}>();

const emit = defineEmits<{
  updated: [];
}>();

const actions = inject(PasswordSettingsActionsKey, null);
const { locale } = useLocale();
const copy = computed(() => passwordSettingsCopy[locale.value]);

type Mode = 'idle' | 'set' | 'reauth-password' | 'reauth-provider';

const mode = ref<Mode>('idle');
const currentPassword = ref('');
const newPassword = ref('');
const pending = ref(false);
type LocalError = Exclude<
  PasswordSettingsMessage,
  PasswordSettingsFailure['kind']
>;
const error = ref<PasswordSettingsFailure | LocalError | null>(null);
const success = ref<'added' | 'changed' | null>(null);
const passwordField = ref<InstanceType<typeof PasswordField> | null>(null);

const passwordSettingsErrorKinds = [
  'reauth-failed',
  'reauth-required',
  'password-invalid',
  'rate-limited',
  'unavailable',
] as const;
const passwordIssues = ['length', 'common', 'breached'] as const;

const errorMessage = computed(() => {
  if (error.value === null) return '';
  if (typeof error.value === 'string') return copy.value.errors[error.value];
  if (error.value.kind === 'password-invalid') {
    return copy.value.passwordPolicy[error.value.issue ?? 'generic'];
  }
  return copy.value.errors[error.value.kind];
});

function isPasswordSettingsErrorKind(
  value: unknown,
): value is PasswordSettingsFailure['kind'] {
  return typeof value === 'string'
    && (passwordSettingsErrorKinds as readonly string[]).includes(value);
}

function isPasswordIssue(
  value: unknown,
): value is NonNullable<PasswordSettingsFailure['issue']> {
  return typeof value === 'string'
    && (passwordIssues as readonly string[]).includes(value);
}

function errorFor(failure: unknown): PasswordSettingsFailure {
  if (
    !(failure instanceof PasswordSettingsFailure)
    || !isPasswordSettingsErrorKind(failure.kind)
  ) {
    return new PasswordSettingsFailure('unavailable');
  }
  return new PasswordSettingsFailure(
    failure.kind,
    failure.kind === 'password-invalid' && isPasswordIssue(failure.issue)
      ? failure.issue
      : undefined,
  );
}

function start(): void {
  error.value = null;
  success.value = null;
  currentPassword.value = '';
  newPassword.value = '';
  mode.value = props.hasPassword ? 'reauth-password' : 'set';
}

function cancel(): void {
  mode.value = 'idle';
  error.value = null;
  currentPassword.value = '';
  newPassword.value = '';
  pending.value = false;
}

async function submitReauth(): Promise<void> {
  if (pending.value || !actions) return;
  if (!currentPassword.value) {
    error.value = 'current-password-required';
    return;
  }
  pending.value = true;
  error.value = null;
  try {
    await actions.reauthenticate(currentPassword.value);
    currentPassword.value = '';
    newPassword.value = '';
    mode.value = 'set';
  } catch (failure) {
    error.value = errorFor(failure);
  } finally {
    pending.value = false;
  }
}

async function submitSet(): Promise<void> {
  if (pending.value || !actions) return;
  if (passwordField.value?.confirmMismatch) {
    error.value = 'passwords-do-not-match';
    return;
  }
  if (!newPassword.value) {
    error.value = 'new-password-required';
    return;
  }
  pending.value = true;
  error.value = null;
  try {
    await actions.setPassword(newPassword.value);
    currentPassword.value = '';
    newPassword.value = '';
    success.value = props.hasPassword ? 'changed' : 'added';
    mode.value = 'idle';
    emit('updated');
  } catch (failure) {
    const normalizedFailure = errorFor(failure);
    if (normalizedFailure.kind === 'reauth-required') {
      currentPassword.value = '';
      newPassword.value = '';
      mode.value = props.hasPassword ? 'reauth-password' : 'reauth-provider';
    }
    error.value = normalizedFailure;
  } finally {
    pending.value = false;
  }
}

async function submitProviderReauth(provider: AuthProvider): Promise<void> {
  if (pending.value || !actions) return;
  pending.value = true;
  error.value = null;
  try {
    await actions.startProviderReauth(provider);
  } catch (failure) {
    error.value = errorFor(failure);
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <div
    data-testid="password-settings"
    class="grid gap-4"
  >
    <h2
      id="password-title"
      class="text-lg font-semibold"
    >
      {{ copy.title }}
    </h2>
    <p data-testid="password-status">
      {{ hasPassword ? copy.hasPassword : copy.noPassword }}
    </p>

    <StatusBanner
      v-if="success"
      kind="success"
      testid="password-success"
    >
      {{ copy.success[success] }}
    </StatusBanner>
    <StatusBanner
      v-if="error"
      kind="error"
      testid="password-error"
      focus-on-mount
    >
      {{ errorMessage }}
    </StatusBanner>

    <Button
      v-if="mode === 'idle'"
      data-testid="password-action"
      type="button"
      @click="start"
    >
      {{ hasPassword ? copy.changePassword : copy.addPassword }}
    </Button>

    <form
      v-else-if="mode === 'set'"
      data-testid="password-form"
      class="grid gap-4"
      novalidate
      @submit.prevent="submitSet"
    >
      <PasswordField
        id="password-new"
        ref="passwordField"
        v-model="newPassword"
        :label="copy.newPassword"
        autocomplete="new-password"
        :confirm-label="copy.confirmPassword"
        confirm
        :locale="locale"
      />
      <div class="flex gap-2">
        <Button
          data-testid="password-set-submit"
          :disabled="pending"
          variant="secondary"
          type="submit"
        >
          {{ pending ? copy.saving : copy.savePassword }}
        </Button>
        <Button
          data-testid="password-cancel"
          type="button"
          variant="ghost"
          @click="cancel"
        >
          {{ copy.cancel }}
        </Button>
      </div>
    </form>

    <form
      v-else-if="mode === 'reauth-password'"
      data-testid="password-form"
      class="grid gap-4"
      novalidate
      @submit.prevent="submitReauth"
    >
      <PasswordField
        id="password-current"
        v-model="currentPassword"
        :label="copy.currentPassword"
        autocomplete="current-password"
        :locale="locale"
      />
      <div class="flex gap-2">
        <Button
          data-testid="password-reauth-submit"
          :disabled="pending"
          variant="secondary"
          type="submit"
        >
          {{ pending ? copy.checking : copy.continue }}
        </Button>
        <Button
          data-testid="password-cancel"
          type="button"
          variant="ghost"
          @click="cancel"
        >
          {{ copy.cancel }}
        </Button>
      </div>
    </form>

    <div
      v-else-if="mode === 'reauth-provider'"
      class="grid gap-3"
    >
      <p>{{ copy.providerReauth }}</p>
      <div class="flex flex-wrap gap-2">
        <Button
          v-for="provider in providers"
          :key="provider"
          :data-testid="`password-provider-reauth-${provider}`"
          :disabled="pending"
          type="button"
          @click="submitProviderReauth(provider)"
        >
          {{ copy.continueWithProvider(providerNames[provider]) }}
        </Button>
        <Button
          data-testid="password-cancel"
          type="button"
          variant="ghost"
          @click="cancel"
        >
          {{ copy.cancel }}
        </Button>
      </div>
    </div>
  </div>
</template>
