<script setup lang="ts">
/**
 * /verify: "Kiểm chứng phiên bản đang chạy" / "Verify what's running"
 * (docs/design/deployment-transparency/page.md and visual.md). Server-
 * rendered with the title, lead, and limits; the browser fills the status,
 * chain, and components after mount.
 */
import { computed, nextTick, provide, ref } from 'vue';
import { useDeploymentDocument } from '@/composables/useDeploymentDocument';
import type {
  DeploymentDocument,
  PageState,
} from '@/utils/deploymentDocument';
import { pageTitle } from '@/i18n/meta';
import { verifyCopy } from '@/i18n/verify';
import VerifyChain from '@/components/verify/VerifyChain.vue';
import VerifyCommands from '@/components/verify/VerifyCommands.vue';
import VerifyComponents from '@/components/verify/VerifyComponents.vue';
import VerifyLimits from '@/components/verify/VerifyLimits.vue';
import VerifyStatusCard from '@/components/verify/VerifyStatusCard.vue';
import { verifyAnnounceKey } from '@/components/verify/verifyAnnounce';

const { locale } = useLocale();
const copy = computed(() => verifyCopy[locale.value]);
const { state, now } = useDeploymentDocument();

useSiteSeo(() => ({
  path: '/verify',
  title: pageTitle(copy.value.title),
  description: copy.value.description,
  locale: locale.value,
}));

const announcement = ref('');
// Clearing first lets a second identical message announce again.
provide(verifyAnnounceKey, (message: string) => {
  announcement.value = '';
  void nextTick(() => {
    announcement.value = message;
  });
});

function documentOf(current: PageState): DeploymentDocument | null {
  return 'document' in current ? current.document : null;
}

const currentDocument = computed(() => documentOf(state.value));
const hidesChain = computed(() => (
  state.value.kind === 'unavailable' || state.value.kind === 'outdated'
));
const isStale = computed(() => state.value.kind === 'stale');
</script>

<template>
  <main
    class="verify-page mx-auto flex w-full max-w-[60rem] flex-col gap-8 px-4
      pt-8 pb-16 min-[42rem]:gap-10 min-[42rem]:px-6 min-[42rem]:pt-12
      min-[42rem]:pb-24"
    data-testid="verify-page"
  >
    <p
      aria-live="polite"
      class="sr-only"
      role="status"
    >
      {{ announcement }}
    </p>
    <div>
      <h1
        class="text-xl font-bold min-[42rem]:text-2xl"
        data-page-title
      >
        {{ copy.title }}
      </h1>
      <p class="mt-2 max-w-[40rem] text-md text-muted-foreground">
        {{ copy.lead }}
      </p>
    </div>

    <VerifyStatusCard
      :locale="locale"
      :now="now"
      :state="state"
    />
    <template v-if="!hidesChain">
      <VerifyChain
        :locale="locale"
        :state="state"
      />
      <VerifyComponents
        :document="currentDocument"
        :locale="locale"
        :stale="isStale"
      />
    </template>
    <VerifyCommands
      :document="currentDocument"
      :locale="locale"
      :state="state"
    />
    <VerifyLimits :locale="locale" />
  </main>
</template>

<style scoped>
.verify-page {
  --verify-verified: var(--link);
  --verify-rolling: var(--brand-indigo);
  --verify-neutral: var(--muted-foreground);
  --verify-bad: var(--destructive);
}
</style>
