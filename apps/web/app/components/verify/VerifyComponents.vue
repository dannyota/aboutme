<script setup lang="ts">
/**
 * The components card (docs/design/deployment-transparency/visual.md,
 * "Component rows"): one row per serving component, plus `maintenance`
 * while it runs, each with its running images, versions, replicas, and
 * signature/SBOM marks.
 */
import { Check, Minus, X } from '@lucide/vue';
import { computed } from 'vue';
import { Skeleton } from '@/components/ui/skeleton';
import type {
  CheckStatus,
  DeploymentDocument,
} from '@/utils/deploymentDocument';
import { componentRows } from '@/utils/verifyView';
import type { Locale } from '@/i18n/locale';
import { verifyCopy } from '@/i18n/verify';
import { cn } from '@/lib/utils';
import VerifyDigestChip from './VerifyDigestChip.vue';

const props = defineProps<{
  /** Null while loading; the card still renders, with skeleton rows. */
  readonly document: DeploymentDocument | null;
  readonly stale: boolean;
  readonly locale: Locale;
}>();

const copy = computed(() => verifyCopy[props.locale]);
const rows = computed(() => (
  props.document === null ? [] : componentRows(props.document)
));
const anyRolling = computed(() => rows.value.some((row) => row.rolling));

/** Stale values are shown but never rechecked (visual.md, component rows). */
function statusOf(status: CheckStatus): CheckStatus {
  return props.stale ? 'unchecked' : status;
}

function statusIcon(status: CheckStatus) {
  if (status === 'verified') return Check;
  if (status === 'not_found' || status === 'invalid') return X;
  return Minus;
}

function statusClass(status: CheckStatus): string {
  if (status === 'verified') return 'text-muted-foreground';
  if (status === 'not_found' || status === 'invalid') {
    return 'font-semibold text-destructive';
  }
  return 'text-muted-foreground';
}
</script>

<template>
  <section
    aria-labelledby="verify-components-heading"
    class="rounded-[20px] border bg-card p-5 shadow-[var(--shadow-product)]
      min-[42rem]:p-7"
    data-testid="verify-components"
  >
    <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
      <h2
        id="verify-components-heading"
        class="text-[18px] font-semibold"
      >
        {{ copy.components.heading }}
      </h2>
      <p
        v-if="anyRolling"
        class="text-[13px] text-muted-foreground"
      >
        {{ copy.components.rollingHint }}
      </p>
    </div>
    <div
      v-if="document === null"
      class="mt-4 grid gap-2"
      role="status"
    >
      <span class="sr-only">{{ copy.loading }}</span>
      <Skeleton
        v-for="line in 3"
        :key="line"
        class="h-10 w-full"
      />
    </div>
    <ul
      v-else
      aria-labelledby="verify-components-heading"
      class="mt-4 divide-y divide-border"
    >
      <li
        v-for="row in rows"
        :key="row.name"
        :class="cn(
          'verify-row relative flex flex-col gap-2.5 py-4 first:pt-0',
          'last:pb-0 min-[42rem]:flex-row min-[42rem]:gap-4',
          row.failing && 'verify-rail verify-rail-bad',
          !row.failing && row.rolling && 'verify-rail verify-rail-rolling',
        )"
        :data-component="row.name"
      >
        <div
          class="flex shrink-0 items-baseline gap-2 min-[42rem]:w-[10rem]
            min-[42rem]:flex-col min-[42rem]:gap-0.5"
        >
          <p class="font-mono text-[15px] font-bold">
            {{ row.name }}
          </p>
          <p class="text-[13px] text-muted-foreground">
            {{ copy.components.roles[row.name] }}
          </p>
        </div>
        <div class="flex-1 space-y-2.5">
          <p
            v-if="row.lines.length === 0"
            class="text-[13px] text-muted-foreground"
          >
            {{ copy.components.noImage }}
          </p>
          <div
            v-for="line in row.lines"
            :key="line.image.digest"
            class="flex flex-col gap-2 text-[13px] min-[42rem]:flex-row
              min-[42rem]:items-center min-[42rem]:gap-0"
          >
            <div class="min-[42rem]:w-[17rem] min-[42rem]:flex-none">
              <VerifyDigestChip
                :component="row.name"
                :digest="line.image.digest"
                :failing="line.failing"
                :locale="locale"
                :version="line.age !== null ? line.image.version : null"
              />
            </div>
            <div class="flex flex-wrap items-center gap-x-3.5 gap-y-1.5">
              <span
                v-if="line.image.version"
                class="font-semibold"
              >{{ line.image.version }}</span>
              <span v-else><span aria-hidden="true">&ndash;</span><span
                class="sr-only"
              >{{ copy.components.versionUnknown }}</span></span>
              <span
                v-if="line.age"
                :class="cn(
                  'rounded-full px-1.5 py-0.5 text-[11px] font-bold',
                  line.age === 'newest'
                    ? 'bg-surface-indigo'
                    : 'bg-muted text-muted-foreground',
                )"
              >{{ line.age === 'newest' ? copy.components.newest
                : copy.components.older }}</span>
              <span class="inline-flex items-center gap-1.5">
                <span
                  aria-hidden="true"
                  class="inline-flex gap-[3px]"
                >
                  <span
                    v-for="square in Math.min(line.image.replicas, 8)"
                    :key="square"
                    :class="cn(
                      'verify-replica size-2',
                      line.age === 'older' ? 'bg-input' : 'bg-brand-blue',
                    )"
                  />
                </span>
                {{ copy.components.replicas(line.image.replicas) }}
              </span>
              <span
                :class="cn(
                  'inline-flex items-center gap-1',
                  statusClass(statusOf(line.image.signature)),
                )"
              >
                <component
                  :is="statusIcon(statusOf(line.image.signature))"
                  aria-hidden="true"
                  :class="cn(
                    'size-3.5',
                    statusOf(line.image.signature) === 'verified'
                      && 'text-link',
                  )"
                />
                {{ copy.components.signature[statusOf(line.image.signature)] }}
              </span>
              <span
                :class="cn(
                  'inline-flex items-center gap-1',
                  statusClass(statusOf(line.image.sbom)),
                )"
              >
                <component
                  :is="statusIcon(statusOf(line.image.sbom))"
                  aria-hidden="true"
                  :class="cn(
                    'size-3.5',
                    statusOf(line.image.sbom) === 'verified' && 'text-link',
                  )"
                />
                {{ copy.components.sbom[statusOf(line.image.sbom)] }}
              </span>
            </div>
          </div>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
/* A 3 px rail on the card's left edge beside a component in rollout or in
   failure (visual.md, component rows). Decorative: the tag and mark text
   carry the state. */
.verify-rail::before {
  content: "";
  position: absolute;
  top: 16px;
  bottom: 16px;
  left: -20px;
  width: 3px;
  border-radius: 0 3px 3px 0;
}

.verify-rail:first-child::before {
  top: 0;
}

.verify-rail:last-child::before {
  bottom: 0;
}

.verify-rail-rolling::before {
  background: var(--brand-indigo);
}

.verify-rail-bad::before {
  background: var(--destructive);
}

@media (min-width: 42rem) {
  .verify-rail::before {
    left: -28px;
  }
}

@media (forced-colors: active) {
  .verify-rail::before,
  .verify-replica {
    forced-color-adjust: none;
    background: CanvasText;
  }
}
</style>
