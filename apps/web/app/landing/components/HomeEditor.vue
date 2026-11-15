<script setup lang="ts">
import type { Locale } from '@/i18n/locale';

import type { LandingCopy } from '../copy';
import EditorIllustration from './EditorIllustration.vue';

defineProps<{
  readonly copy: LandingCopy;
  readonly locale: Locale;
}>();
</script>

<template>
  <section
    aria-labelledby="landing-editor-title"
    class="write home-panel"
    data-testid="landing-editor"
  >
    <div>
      <h2
        id="landing-editor-title"
        class="home-h2"
      >
        {{ copy.editor.title }}
      </h2>
      <p class="home-lead">
        {{ copy.editor.lead }}
      </p>
      <ul
        class="facts"
        data-testid="landing-facts"
      >
        <li
          v-for="fact in copy.editor.facts"
          :key="fact"
        >
          {{ fact }}
        </li>
      </ul>
    </div>
    <EditorIllustration
      :copy="copy"
      :locale="locale"
    />
  </section>
</template>

<style scoped>
.write {
  display: grid;
  gap: 36px;
  background: var(--surface-mist);
}

.facts {
  max-width: 34em;
  margin-top: 24px;
}

.facts li {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 13px 0;
  border-top: 1px solid var(--hairline);
  font-weight: 700;
}

.facts li::before {
  content: "";
  flex: none;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--coin);
  box-shadow: inset 0 -2px 0 var(--coin-edge);
}

@media (min-width: 64rem) {
  .write {
    grid-template-columns: minmax(0, 4fr) minmax(0, 6fr);
    align-items: center;
    gap: 48px;
  }
}

@media (min-width: 80rem) {
  .write {
    grid-template-columns: minmax(0, 5fr) minmax(0, 6fr);
    gap: 64px;
  }
}
</style>
