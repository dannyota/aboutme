<script setup lang="ts">
/**
 * The "verify it yourself" card (docs/design/deployment-transparency/
 * visual.md, "Verify it yourself"): a component selector and two commands
 * with live values, plus links to the JSON document, the transparency log,
 * the build run, and the GitHub CLI manual.
 */
import { ExternalLink, File } from '@lucide/vue';
import { computed, ref } from 'vue';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import {
  deploymentDocumentPath,
  type DeploymentDocument,
  type PageState,
} from '@/utils/deploymentDocument';
import {
  type CommandTarget,
  commandTargets,
  defaultTarget,
  readDigestsCommand,
  verifyImageCommand,
} from '@/utils/verifyView';
import type { Locale } from '@/i18n/locale';
import {
  githubCliManualUrl,
  sbomPredicateFlag,
  verifyCopy,
} from '@/i18n/verify';
import VerifyCommandBlock from './VerifyCommandBlock.vue';

const props = defineProps<{
  readonly document: DeploymentDocument | null;
  readonly state: PageState;
  readonly locale: Locale;
}>();

const copy = computed(() => verifyCopy[props.locale]);

const targets = computed<CommandTarget[]>(
  () => (props.document ? commandTargets(props.document) : []),
);
const defaultKey = computed(
  () => defaultTarget(props.state, targets.value)?.key ?? null,
);
const selectedKey = ref<string | null>(null);
const selected = computed<CommandTarget | null>(() => (
  targets.value.find((target) => target.key === selectedKey.value)
  ?? targets.value.find((target) => target.key === defaultKey.value)
  ?? null
));

function select(key: unknown): void {
  if (typeof key === 'string') selectedKey.value = key;
}

const command2 = computed(() => verifyImageCommand(selected.value));
const buildLink = computed(() => selected.value?.image.links.build ?? null);
const transparencyLogLink = computed(
  () => selected.value?.image.links.transparencyLog ?? null,
);
</script>

<template>
  <section
    aria-labelledby="verify-yourself-heading"
    class="rounded-[20px] border bg-card p-5 shadow-[var(--shadow-product)]
      min-[42rem]:p-7"
    data-testid="verify-yourself"
  >
    <h2
      id="verify-yourself-heading"
      class="text-[18px] font-semibold"
    >
      {{ copy.yourself.heading }}
    </h2>
    <p class="mt-1 max-w-[40rem] text-[15px] text-muted-foreground">
      {{ copy.yourself.intro }}
    </p>

    <div
      v-if="targets.length > 0"
      class="mt-5"
    >
      <p
        id="verify-selector-label"
        class="text-[13px] font-medium text-muted-foreground"
      >
        {{ copy.yourself.selector }}
      </p>
      <ToggleGroup
        aria-labelledby="verify-selector-label"
        class="mt-1.5 flex max-w-full flex-wrap items-center gap-1 rounded-md
          bg-muted p-1"
        data-testid="verify-selector"
        :model-value="selected?.key"
        type="single"
        @update:model-value="select"
      >
        <ToggleGroupItem
          v-for="target in targets"
          :key="target.key"
          class="h-8 font-mono text-[13px] data-[state=on]:bg-card
            data-[state=on]:text-foreground
            data-[state=on]:shadow-[0_1px_1px_rgb(16_27_63/0.12)]"
          :value="target.key"
        >
          {{ target.label }}
        </ToggleGroupItem>
      </ToggleGroup>
    </div>

    <ol class="mt-6 space-y-5">
      <li class="flex gap-3">
        <span
          class="flex size-[22px] flex-none items-center justify-center
            rounded-full bg-surface-blue text-[11px] font-bold text-link"
        >1</span>
        <div class="min-w-0 flex-1">
          <p class="font-semibold">
            {{ copy.yourself.readDigests }}
          </p>
          <VerifyCommandBlock
            class="mt-2"
            :copied-text="copy.copied"
            :copy-failed-text="copy.copyFailed"
            :copy-label="copy.yourself.copyCommand(1)"
            :copy-text="readDigestsCommand.copy"
            :lines="readDigestsCommand.lines"
            :scroll-label="copy.yourself.readDigests"
            testid="verify-command-1"
          />
        </div>
      </li>
      <li class="flex gap-3">
        <span
          class="flex size-[22px] flex-none items-center justify-center
            rounded-full bg-surface-blue text-[11px] font-bold text-link"
        >2</span>
        <div class="min-w-0 flex-1">
          <p class="font-semibold">
            {{ copy.yourself.verifyBuild }}
          </p>
          <VerifyCommandBlock
            class="mt-2"
            :copied-text="copy.copied"
            :copy-failed-text="copy.copyFailed"
            :copy-label="copy.yourself.copyCommand(2)"
            :copy-text="command2.copy"
            :lines="command2.lines"
            :scroll-label="copy.yourself.verifyBuild"
            testid="verify-command-2"
          />
          <p class="mt-2 text-[13px] text-muted-foreground">
            {{ copy.yourself.sbomNote[0] }}<code
              class="font-mono"
            >{{ sbomPredicateFlag }}</code>{{ copy.yourself.sbomNote[1] }}
          </p>
        </div>
      </li>
    </ol>

    <div
      class="mt-6 flex flex-wrap gap-5 border-t pt-5 text-[14px] font-medium
        text-link"
    >
      <a
        class="inline-flex items-center gap-1.5 underline-offset-4
          hover:underline"
        data-testid="verify-json-link"
        :href="deploymentDocumentPath"
      >
        <File
          aria-hidden="true"
          class="size-4"
        />
        {{ copy.yourself.jsonDocument }}
      </a>
      <a
        v-if="transparencyLogLink"
        class="inline-flex items-center gap-1.5 underline-offset-4
          hover:underline"
        :href="transparencyLogLink"
        rel="noopener noreferrer"
      >
        {{ copy.yourself.transparencyLog }}
        <ExternalLink
          aria-hidden="true"
          class="size-4"
        />
      </a>
      <a
        v-if="buildLink"
        class="inline-flex items-center gap-1.5 underline-offset-4
          hover:underline"
        :href="buildLink"
        rel="noopener noreferrer"
      >
        {{ copy.yourself.buildRun }}
        <ExternalLink
          aria-hidden="true"
          class="size-4"
        />
      </a>
      <a
        class="inline-flex items-center gap-1.5 underline-offset-4
          hover:underline"
        :href="githubCliManualUrl"
        rel="noopener noreferrer"
      >
        {{ copy.yourself.cliManual }}
        <ExternalLink
          aria-hidden="true"
          class="size-4"
        />
      </a>
    </div>
  </section>
</template>
