<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import AppSeal from '@/components/app/AppSeal.vue';
import { Button } from '@/components/ui/button';
import { canonicalPublicPath } from '../../editor/publishApi';
import type { PublishControllerState } from '../../editor/publishController';
import type { Locale } from '../../i18n/locale';
import type { PublishCopy } from '../../i18n/publish';
import { workspaceCopy } from '../../i18n/workspace';

const props = defineProps<{
  readonly state: PublishControllerState;
  readonly copy: PublishCopy;
  readonly locale: Locale;
}>();
const copyState = ref<'idle' | 'copied' | 'failed'>('idle');
let copyResetTimer: ReturnType<typeof setTimeout> | undefined;

const publicHref = computed(() => {
  const result = props.state;
  return result.kind === 'accepted' && result.resume.metadata.live
    ? canonicalPublicPath(result.resume.metadata.slug)
    : null;
});
const publicSealLabel = computed(() => {
  const link = publicHref.value;
  return link === null ? '' : workspaceCopy[props.locale].publicAt(link);
});

watch(publicHref, () => resetCopyState());

async function copyLink(): Promise<void> {
  const path = publicHref.value;
  if (path === null) return;
  resetCopyState();
  try {
    await navigator.clipboard.writeText(`${location.origin}${path}`);
    copyState.value = 'copied';
    copyResetTimer = setTimeout(() => {
      copyState.value = 'idle';
      copyResetTimer = undefined;
    }, 2_000);
  } catch {
    copyState.value = 'failed';
  }
}

function resetCopyState(): void {
  if (copyResetTimer !== undefined) {
    clearTimeout(copyResetTimer);
    copyResetTimer = undefined;
  }
  copyState.value = 'idle';
}

onBeforeUnmount(resetCopyState);
</script>

<template>
  <div
    v-if="state.kind === 'accepted'"
    class="publish-dialog__success grid justify-items-start gap-3"
  >
    <p
      aria-live="polite"
      role="status"
    >
      <template v-if="state.resume.metadata.live">
        {{ copy.published }}
      </template>
      <template v-else>
        {{ copy.private }}
      </template>
    </p>
    <template v-if="publicHref !== null">
      <AppSeal
        :link="publicHref"
        :label="publicSealLabel"
        :locale="locale"
        size="stamp"
      />
      <a
        :href="publicHref"
        class="text-link underline underline-offset-4"
        data-action="view-public-resume"
      >aboutme.vn{{ publicHref }}</a>
      <Button
        data-action="copy-link"
        type="button"
        variant="secondary"
        @click="copyLink"
      >
        {{ copyState === 'copied' ? copy.copied : copy.copyLink }}
      </Button>
      <p
        v-if="copyState === 'failed'"
        data-testid="copy-link-error"
        role="alert"
      >
        {{ copy.copyFailed }}
      </p>
    </template>
  </div>
</template>
