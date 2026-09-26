<script setup lang="ts">
/**
 * The chain card (docs/design/deployment-transparency/visual.md, "Chain"):
 * Source, Build, Image, and Running as an ordered list. Each step has a
 * disk, a label, a value, a muted line, and a state chip; a connector joins
 * each disk to the next, across below 42 rem and down the disk column on
 * phones. Connectors are decorative; steps 2 to 4 carry their meaning in
 * visually hidden text.
 */
import {
  ArrowRightLeft,
  Check,
  GitCommitHorizontal,
  Minus,
  Package,
  Server,
  Workflow,
  X,
} from '@lucide/vue';
import { computed } from 'vue';
import { Skeleton } from '@/components/ui/skeleton';
import {
  type PageState,
  servingComponents,
} from '@/utils/deploymentDocument';
import {
  buildRunId,
  chainView,
  type ChipKind,
  type ConnectorKind,
  shortCommit,
} from '@/utils/verifyView';
import type { Locale } from '@/i18n/locale';
import {
  type ChainStep,
  clockTime,
  platformPlace,
  sourceRepositoryName,
  sourceRepositoryUrl,
  verifyCopy,
} from '@/i18n/verify';
import { cn } from '@/lib/utils';

const props = defineProps<{
  readonly state: PageState;
  readonly locale: Locale;
}>();

const copy = computed(() => verifyCopy[props.locale]);
const loading = computed(() => props.state.kind === 'loading');
const view = computed(() => chainView(props.state));
const document = computed(() =>
  'document' in props.state ? props.state.document : null);

const steps: readonly ChainStep[] = ['source', 'build', 'image', 'running'];
const stepIcons = {
  source: GitCommitHorizontal,
  build: Workflow,
  image: Package,
  running: Server,
} as const;

const chipIcons: Record<ChipKind, typeof Check> = {
  match: Check,
  updating: ArrowRightLeft,
  not_rechecked: Minus,
  not_verified: Minus,
  failed: X,
};

const chipClass: Record<ChipKind, string> = {
  match: 'border-transparent bg-surface-blue text-link',
  updating: 'border-transparent bg-surface-indigo text-foreground',
  not_rechecked: 'border-border text-muted-foreground',
  not_verified: 'border-border text-muted-foreground',
  failed: 'border-transparent bg-surface-destructive text-destructive',
};

const connectorIcons: Record<ConnectorKind, typeof Check | null> = {
  agree: Check,
  updating: ArrowRightLeft,
  unknown: null,
  disagree: X,
};

function chip(index: number): ChipKind {
  return view.value?.chips[index] ?? 'not_verified';
}

function connector(index: number): ConnectorKind {
  return view.value?.connectors[index] ?? 'unknown';
}

function chipLabel(index: number): string {
  const kind = chip(index);
  if (kind !== 'failed') return copy.value.chain.chips[kind];
  const failure = view.value?.failure;
  return failure
    ? copy.value.chain.failed[failure.reason](failure.component)
    : '';
}

const imageCount = computed(() => (document.value?.components ?? [])
  .filter(({ name }) => servingComponents.includes(name))
  .reduce((sum, { runningImages }) => sum + runningImages.length, 0));

const release = computed(() => view.value?.release ?? null);
const buildLink = computed(() => release.value?.image.links.build ?? null);
</script>

<template>
  <section
    aria-labelledby="verify-chain-heading"
    class="rounded-[20px] border bg-card p-5 shadow-[var(--shadow-product)]
      min-[42rem]:p-7"
    data-testid="verify-chain"
  >
    <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
      <h2
        id="verify-chain-heading"
        class="text-[18px] font-semibold"
      >
        {{ copy.chain.heading }}
      </h2>
      <p class="text-[13px] text-muted-foreground">
        {{ copy.chain.hint }}
      </p>
    </div>
    <ol class="verify-chain mt-4">
      <li
        v-for="(step, index) in steps"
        :key="step"
        class="verify-step"
        :data-step="step"
      >
        <span
          v-if="index < steps.length - 1"
          aria-hidden="true"
          class="verify-connector"
          :data-connector="connector(index)"
        >
          <span
            v-if="connectorIcons[connector(index)]"
            class="verify-connector-dot"
          >
            <component
              :is="connectorIcons[connector(index)]!"
              class="size-3"
            />
          </span>
        </span>
        <span
          :class="cn(
            'relative z-[1] flex size-10 flex-none items-center justify-center',
            'rounded-full border bg-muted',
            chip(index) === 'failed'
              ? 'border-destructive text-destructive'
              : 'border-border text-muted-foreground',
          )"
          aria-hidden="true"
        >
          <component
            :is="stepIcons[step]"
            class="size-5"
          />
        </span>
        <div class="min-w-0">
          <span
            v-if="index > 0 && copy.chain.connectors[connector(index - 1)]"
            class="sr-only"
          >{{ copy.chain.connectors[connector(index - 1)] }}.</span>
          <p class="text-[13px] text-muted-foreground">
            {{ copy.chain.steps[step] }}
          </p>
          <template v-if="loading || view === null">
            <Skeleton class="mt-1 h-5 w-24" />
            <Skeleton class="mt-1.5 h-4 w-36" />
            <Skeleton class="mt-2 h-6 w-20 rounded-full" />
          </template>
          <template v-else>
            <p
              v-if="step === 'source'"
              class="text-md font-semibold"
            >
              {{ release?.version ?? '–' }}
            </p>
            <p
              v-else-if="step === 'build'"
              class="text-md font-semibold"
            >
              {{ copy.chain.buildValue }}
            </p>
            <p
              v-else-if="step === 'image'"
              class="font-mono text-[14px] font-semibold"
            >
              ghcr.io/dannyota/<wbr>aboutme-*
            </p>
            <p
              v-else
              class="text-md font-semibold"
            >
              aboutme.vn
            </p>
            <p class="mt-0.5 text-[13px] text-muted-foreground">
              <template v-if="step === 'source' && release">
                <a
                  class="verify-link"
                  :href="sourceRepositoryUrl"
                  rel="noopener noreferrer"
                >{{ sourceRepositoryName }}</a>
                @
                <a
                  v-if="release.image.links.commit"
                  class="verify-link font-mono"
                  :href="release.image.links.commit"
                  rel="noopener noreferrer"
                >{{ shortCommit(release.commit) }}</a>
                <span
                  v-else
                  class="font-mono"
                >{{ shortCommit(release.commit) }}</span>
              </template>
              <a
                v-else-if="step === 'build' && buildLink"
                class="verify-link"
                :href="buildLink"
                rel="noopener noreferrer"
              >{{ copy.chain.buildLine(buildRunId(buildLink) ?? '') }}</a>
              <template v-else-if="step === 'image'">
                {{ copy.chain.imageLine(imageCount) }}
              </template>
              <template v-else-if="step === 'running' && document">
                {{ copy.chain.runningLine(
                  platformPlace(document),
                  view.replicas,
                  view.runningSince
                    ? clockTime(view.runningSince, locale)
                    : '–',
                ) }}
              </template>
            </p>
            <span
              :class="cn(
                'mt-2 inline-flex h-6 items-center gap-1 rounded-full border',
                'px-2 text-xs font-semibold',
                chipClass[chip(index)],
              )"
              :data-chip="chip(index)"
            >
              <component
                :is="chipIcons[chip(index)]"
                aria-hidden="true"
                :class="cn(
                  'size-3.5',
                  chip(index) === 'updating' && 'text-brand-indigo',
                )"
              />
              {{ chipLabel(index) }}
            </span>
          </template>
        </div>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.verify-chain {
  display: grid;
  gap: 32px;
}

.verify-step {
  position: relative;
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr);
  gap: 16px;
}

.verify-link {
  color: var(--link);
  text-underline-offset: 4px;
}

.verify-link:hover {
  text-decoration: underline;
}

.verify-link:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
  border-radius: 2px;
}

/* Below 42 rem the connector runs down the disk column, from this disk's
   bottom edge through the 32 px gap to the next disk's top edge. */
.verify-connector {
  position: absolute;
  top: 40px;
  bottom: -32px;
  left: 0;
  width: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  --line: var(--input);
  background-image: linear-gradient(var(--line), var(--line));
  background-position: center;
  background-repeat: no-repeat;
  background-size: 2px 100%;
}

.verify-connector[data-connector="agree"] {
  --line: var(--link);
}

.verify-connector[data-connector="updating"] {
  --line: var(--brand-indigo);
}

.verify-connector[data-connector="unknown"],
.verify-connector[data-connector="disagree"] {
  background-image: repeating-linear-gradient(
    to bottom,
    var(--line) 0 4px,
    transparent 4px 8px
  );
}

.verify-connector[data-connector="disagree"] {
  --line: var(--destructive);
}

.verify-connector-dot {
  display: flex;
  width: 20px;
  height: 20px;
  align-items: center;
  justify-content: center;
  border: 2px solid var(--line);
  border-radius: 9999px;
  background: var(--card);
  color: var(--line);
}

/* From 42 rem the steps are four equal columns 40 px apart, and a connector
   runs from this disk's right edge to the next disk's left edge. */
@media (min-width: 42rem) {
  .verify-chain {
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 40px;
  }

  .verify-step {
    grid-template-columns: minmax(0, 1fr);
    align-content: start;
    gap: 12px;
  }

  .verify-connector {
    top: 0;
    bottom: auto;
    left: 40px;
    width: 100%;
    height: 40px;
    background-size: 100% 2px;
  }

  .verify-connector[data-connector="unknown"],
  .verify-connector[data-connector="disagree"] {
    background-image: repeating-linear-gradient(
      to right,
      var(--line) 0 4px,
      transparent 4px 8px
    );
  }
}

@media (forced-colors: active) {
  .verify-connector,
  .verify-connector-dot {
    --line: CanvasText;
    forced-color-adjust: none;
  }

  .verify-connector-dot {
    background: Canvas;
  }
}
</style>
