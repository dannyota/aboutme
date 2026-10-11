<script setup lang="ts">
/**
 * The status card (docs/design/deployment-transparency/visual.md, "Status
 * card"): the state's mark, title, one detail line, the meta row, and the
 * release pill. Only the title and detail sit in the `role="status"` region,
 * so a refetch that keeps the same state announces nothing; the meta row's
 * relative age is hidden from screen readers beside a static time.
 */
import {
  ArrowRightLeft,
  CircleDashed,
  Clock,
  Info,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
} from '@lucide/vue';
import { computed } from 'vue';
import { Button } from '@/components/ui/button';
import type { PageState, PageStateKind } from '@/utils/deploymentDocument';
import { chainView, shortCommit } from '@/utils/verifyView';
import type { Locale } from '@/i18n/locale';
import { clockTime, verifyCopy } from '@/i18n/verify';
import { cn } from '@/lib/utils';

const props = defineProps<{
  readonly state: PageState;
  readonly now: number | null;
  readonly locale: Locale;
}>();

const copy = computed(() => verifyCopy[props.locale]);
const document = computed(() =>
  'document' in props.state ? props.state.document : null);

interface Mark {
  readonly icon: typeof Info;
  readonly filled: boolean;
}

const marks: Record<PageStateKind, Mark | null> = {
  loading: null,
  unavailable: { icon: Info, filled: false },
  outdated: { icon: RefreshCw, filled: false },
  stale: { icon: Clock, filled: false },
  mismatch: { icon: ShieldAlert, filled: true },
  unverified: { icon: CircleDashed, filled: false },
  rolling_out: { icon: ArrowRightLeft, filled: true },
  verified: { icon: ShieldCheck, filled: true },
};

const mark = computed(() => marks[props.state.kind]);

function ageSeconds(observedAt: string): number | null {
  if (props.now === null) return null;
  return Math.max(0, Math.floor((props.now - Date.parse(observedAt)) / 1000));
}

const text = computed(() => {
  const state = props.state;
  const c = copy.value;
  switch (state.kind) {
    case 'loading':
      return { title: c.loading, detail: null };
    case 'unavailable':
      return c.unavailable;
    case 'outdated':
      return { title: c.outdated.title, detail: c.outdated.detail };
    case 'stale': {
      const seconds = ageSeconds(state.document.observedAt);
      return {
        title: c.stale.title(
          clockTime(state.document.observedAt, props.locale)),
        detail: seconds === null ? null : c.stale.detail(c.span(seconds)),
      };
    }
    case 'mismatch': {
      if (state.reason === 'summary' || state.component === null) {
        return { title: c.mismatch.title, detail: c.mismatch.summary };
      }
      const detail = c.mismatch.detail[state.reason](state.component);
      return {
        title: c.mismatch.title,
        detail: state.more > 0
          ? `${detail.replace(/\.$/u, '')} ${c.mismatch.more(state.more)}.`
          : detail,
      };
    }
    case 'unverified':
      return {
        title: c.unverified.title,
        detail: c.unverified.detail(state.component),
      };
    case 'rolling_out':
      return {
        title: c.rollingOut.title,
        detail: c.rollingOut.detail(state.component, state.from, state.to),
      };
    case 'verified':
      return {
        title: c.verified.title,
        detail: state.release
          ? c.verified.detail(
              3, state.release.version, shortCommit(state.release.commit))
          : c.verified.detailMixed,
      };
    default:
      return { title: '', detail: null };
  }
});

const pill = computed(() => {
  const state = props.state;
  if (state.kind === 'rolling_out') {
    return { text: `${state.from} → ${state.to}`, commit: null };
  }
  if (state.kind === 'stale') {
    const release = chainView(state)?.release;
    return release
      ? { text: copy.value.stale.pill(release.version), commit: null }
      : null;
  }
  if (state.kind === 'verified' && state.release) {
    return {
      text: state.release.version,
      commit: shortCommit(state.release.commit),
    };
  }
  return null;
});

const checkedTime = computed(() => document.value
  ? clockTime(document.value.observedAt, props.locale)
  : null);

const relativeAge = computed(() => {
  if (document.value === null) return null;
  const seconds = ageSeconds(document.value.observedAt);
  if (seconds === null) return null;
  return seconds < 60
    ? copy.value.justNow
    : copy.value.ago(copy.value.span(seconds));
});

const cardClass: Record<PageStateKind, string> = {
  loading: 'bg-muted',
  unavailable: 'bg-muted',
  outdated: 'bg-muted',
  stale: 'bg-muted',
  unverified: 'bg-muted',
  mismatch: 'verify-status-mismatch bg-surface-destructive',
  rolling_out: 'bg-surface-indigo',
  verified: 'bg-surface-blue',
};

const diskClass: Record<PageStateKind, string> = {
  loading: '',
  unavailable: 'verify-ring',
  outdated: 'verify-ring',
  stale: 'verify-ring',
  unverified: 'verify-ring',
  mismatch: 'verify-disk bg-destructive text-white dark:text-background',
  rolling_out: 'verify-disk bg-brand-indigo text-white dark:text-background',
  verified: 'verify-disk bg-link text-card',
};

function reload(): void {
  window.location.reload();
}
</script>

<template>
  <div
    :class="cn(
      'verify-status grid items-start rounded-[20px] border p-5',
      'shadow-[var(--shadow-product)] min-[42rem]:p-7',
      mark ? 'verify-status-marked' : '',
      cardClass[state.kind],
    )"
    :data-state="state.kind"
    data-testid="verify-status"
  >
    <span
      v-if="mark"
      aria-hidden="true"
      :class="cn(
        'verify-mark flex items-center justify-center rounded-full',
        diskClass[state.kind],
      )"
    >
      <component
        :is="mark.icon"
        class="verify-mark-icon"
      />
    </span>
    <div class="verify-status-text min-w-0">
      <div role="status">
        <h2 class="sr-only">
          {{ copy.statusHeading }}
        </h2>
        <p class="text-lg font-bold min-[42rem]:text-xl">
          {{ text.title }}
        </p>
        <p
          v-if="text.detail"
          :class="cn(
            'mt-1 text-[15px]',
            state.kind === 'mismatch' && 'text-destructive',
          )"
        >
          {{ text.detail }}
        </p>
      </div>
      <p
        v-if="checkedTime"
        class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[13px]
          text-muted-foreground"
      >
        <span>{{
          state.kind === 'stale'
            ? copy.lastCheckedAt(checkedTime)
            : copy.checkedAt(checkedTime)
        }}</span>
        <template v-if="relativeAge">
          <span aria-hidden="true">{{ relativeAge }}</span>
          <time
            class="sr-only"
            :datetime="document?.observedAt"
          >{{ document?.observedAt }}</time>
        </template>
        <span v-if="state.kind !== 'stale'">{{ copy.refreshes }}</span>
      </p>
    </div>
    <div
      v-if="pill || state.kind === 'outdated'"
      class="verify-status-pill"
    >
      <Button
        v-if="state.kind === 'outdated'"
        variant="outline"
        @click="reload"
      >
        {{ copy.outdated.reload }}
      </Button>
      <span
        v-else-if="pill"
        class="inline-flex h-7 items-center gap-1.5 whitespace-nowrap
          rounded-full border bg-card px-3 text-[13px] font-semibold"
        data-testid="verify-pill"
      >
        {{ pill.text }}
        <code
          v-if="pill.commit"
          class="font-mono"
        >{{ pill.commit }}</code>
      </span>
    </div>
  </div>
</template>

<style scoped>
.verify-status {
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
}

.verify-status-marked {
  grid-template-columns: 40px minmax(0, 1fr);
}

.verify-status-marked .verify-status-pill {
  grid-column: 2;
}

.verify-mark {
  width: 40px;
  height: 40px;
}

.verify-mark-icon {
  width: 22px;
  height: 22px;
}

.verify-ring {
  border: 2px solid var(--verify-neutral);
  color: var(--verify-neutral);
}

.verify-status-mismatch {
  border-color: color-mix(in srgb, var(--destructive) 45%, var(--border));
}

@media (min-width: 42rem) {
  .verify-status {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 20px;
  }

  .verify-status-marked {
    grid-template-columns: 56px minmax(0, 1fr) auto;
  }

  .verify-status-marked .verify-status-pill {
    grid-column: 3;
    grid-row: 1;
  }

  .verify-mark {
    width: 56px;
    height: 56px;
  }

  .verify-mark-icon {
    width: 28px;
    height: 28px;
  }
}

@media (forced-colors: active) {
  .verify-status {
    background: Canvas;
  }

  .verify-mark {
    forced-color-adjust: none;
    border: 2px solid CanvasText;
    background: Canvas;
    color: CanvasText;
  }
}
</style>
