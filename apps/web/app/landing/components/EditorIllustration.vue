<script setup lang="ts">
import { computed } from 'vue';

import type { Locale } from '@/i18n/locale';
import { samplePageImage } from '@/templates/pageImages';

import type { LandingCopy } from '../copy';
import { EDITOR_PREVIEW_TEMPLATE } from '../showcase';
import HomeGlyph from './HomeGlyph.vue';

const props = defineProps<{
  readonly copy: LandingCopy;
  readonly locale: Locale;
}>();

const preview = computed(() =>
  samplePageImage(EDITOR_PREVIEW_TEMPLATE, props.locale));
const lastField = computed(() => props.copy.editor.fields.length - 1);
</script>

<template>
  <div data-testid="landing-editor-illustration">
    <div
      :aria-label="copy.editor.illustrationLabel"
      class="editor"
      role="img"
    >
      <div class="form">
        <div class="tabs">
          <span
            v-for="(tab, index) in copy.editor.tabs"
            :key="tab"
            :class="{ on: index === 0 }"
          >{{ tab }}</span>
        </div>
        <div
          v-for="(field, index) in copy.editor.fields"
          :key="field.label"
        >
          <span class="label">{{ field.label }}</span>
          <span
            :class="{ focus: index === lastField }"
            class="input"
          >{{ field.value }}</span>
        </div>
        <span class="saved">
          <HomeGlyph name="saved" />
          <span>{{ copy.editor.saved }}</span>
        </span>
      </div>
      <div class="preview">
        <img
          v-if="preview"
          alt=""
          decoding="async"
          :height="preview.height"
          loading="lazy"
          :src="preview.src"
          :width="preview.width"
        >
      </div>
    </div>
    <p
      class="note"
      data-testid="landing-editor-note"
    >
      {{ copy.editor.note }}
    </p>
  </div>
</template>

<style scoped>
.editor {
  display: grid;
  gap: 18px;
  padding: 16px;
  border-radius: var(--radius-card);
  background: var(--card);
  box-shadow: var(--shadow-card);
}

.form {
  display: grid;
  align-content: start;
  gap: 13px;
  font: 400 14px/1.4 var(--font-paper);
}

.tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 2px;
}

.tabs span {
  padding: 6px 12px;
  border-radius: 999px;
  background: var(--surface-mist);
  font-size: 13px;
  font-weight: 600;
}

.tabs .on {
  background: var(--foreground);
  color: var(--background);
}

.label {
  display: block;
  margin-bottom: 5px;
  font-size: 13px;
  font-weight: 600;
}

.input {
  display: block;
  min-height: 40px;
  padding: 9px 12px;
  border: 1.5px solid var(--control);
  border-radius: 10px;
}

.input.focus {
  border-color: var(--foreground);
  box-shadow: 0 0 0 3px var(--hairline);
}

.input.focus::after {
  content: "";
  display: inline-block;
  width: 1.5px;
  height: 1.05em;
  margin-left: 1px;
  background: var(--foreground);
  vertical-align: -2px;
}

.saved {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--muted-foreground);
  font-size: 13px;
}

.saved svg {
  width: 15px;
  height: 15px;
}

.preview {
  align-self: start;
  padding: 14px;
  border-radius: 14px;
  background: var(--surface-mist);
}

.preview img {
  display: block;
  width: 100%;
  height: auto;
  border-radius: 2px;
  box-shadow: 0 10px 20px -12px rgba(0, 0, 0, 0.5);
}

.note {
  margin-top: 14px;
  color: var(--muted-foreground);
  font-size: 14px;
  line-height: 1.5;
}

@media (min-width: 28rem) {
  .editor {
    grid-template-columns: minmax(0, 10fr) minmax(0, 11fr);
    padding: 20px;
  }
}
</style>
