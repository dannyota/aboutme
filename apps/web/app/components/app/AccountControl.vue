<script setup lang="ts">
import { UserRound } from '@lucide/vue';

const { user } = useAuth();
const accountName = computed(() => user.value?.name?.trim()
  || user.value?.email
  || 'Account');
const accountLabel = computed(() => user.value === null
  ? 'Account settings'
  : `Account settings for ${accountName.value}`);
</script>

<template>
  <NuxtLink
    class="account-control"
    to="/app/settings/sessions"
    :aria-label="accountLabel"
  >
    <span
      class="account-control__avatar"
      aria-hidden="true"
    >
      <UserRound :size="16" />
    </span>
    <span class="account-control__label">{{ accountName }}</span>
  </NuxtLink>
</template>
