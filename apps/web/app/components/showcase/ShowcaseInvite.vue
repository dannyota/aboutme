<script setup lang="ts">
import { computed } from 'vue';

import { buttonVariants } from '@/components/ui/button';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import { cn } from '@/lib/utils';

// The last cell of the grid in the list state: an invitation to publish, not
// a tile (docs/design/ui/showcase.md, Invite card). The target is the empty
// action's: the visitor's resumes when signed in, else sign-up or sign-in.
const props = defineProps<{
  readonly locale: Locale;
  readonly to: string;
}>();
const copy = computed(() => showcaseCopy[props.locale]);
const buttonClass = cn(
  buttonVariants({ variant: 'default' }),
  'h-11 justify-self-start px-4 text-[15px] font-semibold',
);
</script>

<template>
  <li
    class="showcase-invite"
    data-testid="showcase-invite"
  >
    <h2 class="text-lg font-bold text-foreground">
      {{ copy.inviteTitle }}
    </h2>
    <p class="text-[15px] text-muted-foreground">
      {{ copy.inviteBody }}
    </p>
    <NuxtLink
      :class="buttonClass"
      data-action="showcase-invite-create"
      :to="to"
    >
      {{ copy.inviteButton }}
    </NuxtLink>
  </li>
</template>

<style scoped>
.showcase-invite {
  display: grid;
  align-content: center;
  gap: 12px;
  padding: 20px 16px;
  border: 1px dashed var(--input);
  border-radius: var(--radius-dialog);
  background: var(--surface-sand);
}

@media (width >= 641px) {
  .showcase-invite {
    padding: 24px;
  }
}
</style>
