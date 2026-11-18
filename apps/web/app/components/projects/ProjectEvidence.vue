<script setup lang="ts">
import { Plus } from '@lucide/vue';
import { computed, nextTick, ref } from 'vue';

import { Button } from '@/components/ui/button';
import type { Locale } from '@/i18n/locale';
import { projectEvidenceCopy } from '@/i18n/project-evidence';
import { projectsCopy } from '@/i18n/projects';
import { evidenceNoticeText } from './evidenceText';
import type {
  ProjectEvidenceController,
  SectionOutcome,
} from './projectEvidenceState';
import ProjectEvidenceItem from './ProjectEvidenceItem.vue';

const props = defineProps<{
  readonly controller: ProjectEvidenceController;
  readonly locale: Locale;
}>();
const emit = defineEmits<{ add: [opener: HTMLElement] }>();

const list = ref<HTMLElement | null>(null);
const heading = ref<HTMLElement | null>(null);
const copy = computed(() => projectEvidenceCopy[props.locale]);
const saveFirst = computed(() => projectsCopy[props.locale].saveFirst);
const items = computed(() => props.controller.items.value);
const unsaved = computed(() => props.controller.unsaved.value);
const busy = computed(() => props.controller.locked.value
  || props.controller.pending.value !== null);
const status = computed(() => {
  const current = props.controller.status.value;
  return current === null ? '' : evidenceNoticeText(current, props.locale);
});

function control(id: string, action: string): HTMLButtonElement | null {
  return list.value?.querySelector<HTMLButtonElement>(
    `[data-evidence-item="${CSS.escape(id)}"] [data-action="${action}"]`,
  ) ?? null;
}

async function settleFocus(
  outcome: SectionOutcome,
  id: string,
  actions: readonly string[],
): Promise<void> {
  if (outcome === 'session-ended' || outcome === 'closed') return;
  await nextTick();
  const target = actions.map((action) => control(id, action))
    .find((button) => button !== null && !button.disabled);
  (target ?? heading.value)?.focus();
}

async function move(id: string, step: -1 | 1): Promise<void> {
  const outcome = await props.controller.move(id, step);
  const first = step === -1 ? 'evidence-move-up' : 'evidence-move-down';
  const second = step === -1 ? 'evidence-move-down' : 'evidence-move-up';
  await settleFocus(outcome, id, [first, second]);
}

async function recheck(id: string): Promise<void> {
  await settleFocus(await props.controller.recheck(id), id,
    ['evidence-recheck']);
}

async function remove(id: string): Promise<void> {
  await settleFocus(await props.controller.remove(id), id,
    ['evidence-remove']);
}

function add(event: MouseEvent): void {
  if (event.currentTarget instanceof HTMLElement) {
    emit('add', event.currentTarget);
  }
}
</script>

<template>
  <section
    aria-labelledby="project-evidence-title"
    class="grid gap-3 border-t py-8"
    data-testid="project-evidence"
  >
    <h2
      id="project-evidence-title"
      ref="heading"
      class="text-lg font-bold"
      tabindex="-1"
    >
      {{ copy.section }}
    </h2>
    <p
      v-if="unsaved"
      id="project-evidence-save-first"
      class="text-sm"
      data-testid="project-evidence-save-first"
    >
      {{ saveFirst }}
    </p>
    <ul
      v-if="items.length > 0"
      ref="list"
      aria-labelledby="project-evidence-title"
      class="divide-y divide-hairline rounded-[var(--radius-card)] border
        border-border bg-card"
    >
      <ProjectEvidenceItem
        v-for="(item, index) in items"
        :key="item.id"
        :described-by="unsaved ? 'project-evidence-save-first' : undefined"
        :disabled="busy"
        :first="index === 0"
        :item="item"
        :last="index === items.length - 1"
        :locale="locale"
        @move="(step) => move(item.id, step)"
        @recheck="recheck(item.id)"
        @remove="remove(item.id)"
      />
    </ul>
    <p
      v-if="controller.atLimit.value"
      class="text-sm"
      data-testid="project-evidence-limit"
    >
      {{ copy.limit }}
    </p>
    <div>
      <Button
        id="project-evidence-add"
        :disabled="controller.atLimit.value || controller.stale.value"
        type="button"
        variant="outline"
        @click="add"
      >
        <Plus aria-hidden="true" />
        {{ copy.add }}
      </Button>
    </div>
    <p
      class="text-sm empty:hidden"
      data-testid="project-evidence-status"
      role="status"
    >
      <template v-if="status !== ''">
        {{ status }}
      </template>
    </p>
  </section>
</template>
