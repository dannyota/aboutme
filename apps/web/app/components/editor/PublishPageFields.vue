<script setup lang="ts">
/**
 * `PublishPageFields` — the optional tab title and tab icon of the public
 * page, with a preview of the browser tab. Empty fields mean the defaults.
 */
import { computed, reactive, watch } from 'vue';

import TextField from '@/components/app/TextField.vue';
import { Button } from '@/components/ui/button';
import {
  faviconEmojiIssue,
  graphemeCount,
  PUBLIC_TITLE_MAX,
  publicPageIssueField,
  type PublicPageFields,
  type FaviconEmojiIssue,
  type PublicTitleIssue,
  publicTitleIssue,
} from '../../editor/publicPageMeta';
import type { PublishPageCopy } from '../../i18n/publish';

const props = defineProps<{
  /** The default title the page uses when the field is empty. */
  readonly defaultTitle: string;
  readonly disabled?: boolean;
  /** The last publish issues; ones on these fields show until edited. */
  readonly serverIssues: readonly { path: string; code: string }[];
  readonly copy: PublishPageCopy;
}>();

const title = defineModel<string>('title', { required: true });
const emoji = defineModel<string>('emoji', { required: true });

const QUICK_EMOJI = ['📄', '💼', '🚀', '⭐', '🎓', '💻', '🌱', '✨'];

const edited = reactive({ publicTitle: false, faviconEmoji: false });
watch(() => props.serverIssues, () => {
  edited.publicTitle = false;
  edited.faviconEmoji = false;
});
watch(title, () => {
  edited.publicTitle = true;
});
watch(emoji, () => {
  edited.faviconEmoji = true;
});

function serverIssue(field: keyof PublicPageFields): string | undefined {
  if (edited[field]) return undefined;
  return props.serverIssues
    .find((issue) => publicPageIssueField(issue.path) === field)?.code;
}

const count = computed(() => graphemeCount(title.value.trim()));
function titleIssueCopy(code: string | undefined): string | undefined {
  if (code === undefined) return undefined;
  if (code === 'too_long' || code === 'invalid_characters') {
    return props.copy.titleError[code as PublicTitleIssue];
  }
  return props.copy.titleError.generic;
}

function emojiIssueCopy(code: string | undefined): string | undefined {
  if (code === undefined) return undefined;
  if (code === 'invalid_emoji') {
    return props.copy.emojiError[code as FaviconEmojiIssue];
  }
  return props.copy.emojiError.generic;
}

const titleError = computed(() => {
  const local = publicTitleIssue(title.value);
  if (local !== null) return props.copy.titleError[local];
  return titleIssueCopy(serverIssue('publicTitle'));
});
const emojiError = computed(() => {
  const local = faviconEmojiIssue(emoji.value);
  if (local !== null) return props.copy.emojiError[local];
  return emojiIssueCopy(serverIssue('faviconEmoji'));
});
const previewTitle = computed(() =>
  title.value.trim() === '' ? props.defaultTitle : title.value.trim());
const previewEmoji = computed(() =>
  emojiError.value === undefined ? emoji.value.trim() : '');
</script>

<template>
  <fieldset
    class="grid gap-4"
    data-testid="publish-page-fields"
  >
    <legend class="mb-1 text-sm font-medium">
      {{ copy.legend }}
    </legend>
    <TextField
      v-model="title"
      :control-attrs="{ 'data-action': 'publish-public-title' }"
      :disabled="disabled"
      :error="titleError"
      :hint="copy.titleHint(count, PUBLIC_TITLE_MAX)"
      :label="copy.title"
      name="publicTitle"
      :placeholder="defaultTitle"
    />
    <div class="grid gap-2">
      <TextField
        v-model="emoji"
        autocomplete="off"
        :control-attrs="{ 'data-action': 'publish-favicon-emoji' }"
        :disabled="disabled"
        :error="emojiError"
        :hint="copy.emojiHint"
        :label="copy.emoji"
        name="faviconEmoji"
      />
      <div
        :aria-label="copy.suggestedIcons"
        class="flex flex-wrap gap-1"
        role="group"
      >
        <Button
          v-for="item in QUICK_EMOJI"
          :key="item"
          :aria-label="copy.useIcon(item)"
          :aria-pressed="emoji.trim() === item"
          class="text-base"
          data-action="publish-favicon-pick"
          :disabled="disabled"
          size="icon-sm"
          type="button"
          variant="outline"
          @click="emoji = item"
        >
          {{ item }}
        </Button>
        <Button
          v-if="emoji !== ''"
          data-action="publish-favicon-clear"
          :disabled="disabled"
          size="sm"
          type="button"
          variant="ghost"
          @click="emoji = ''"
        >
          {{ copy.removeIcon }}
        </Button>
      </div>
    </div>
    <figure class="grid gap-1">
      <div
        aria-hidden="true"
        class="flex max-w-72 items-center gap-2 rounded-t-md border
          border-b-0 bg-muted px-3 py-1.5 text-sm"
        data-testid="publish-tab-preview"
      >
        <span
          v-if="previewEmoji !== ''"
          class="text-base leading-none"
          data-tab-icon
        >{{ previewEmoji }}</span>
        <img
          v-else
          alt=""
          class="size-4"
          data-tab-icon-default
          src="/favicon.svg"
        >
        <span
          class="min-w-0 truncate"
          data-tab-title
        >{{ previewTitle }}</span>
      </div>
      <figcaption class="text-xs text-muted-foreground">
        {{ copy.tabPreview }}
      </figcaption>
    </figure>
  </fieldset>
</template>
