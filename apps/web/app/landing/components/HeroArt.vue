<script setup lang="ts">
import AppSeal from '@/components/app/AppSeal.vue';
import ResumeDocument from '@/components/resume/ResumeDocument.vue';
import type { Locale } from '@/i18n/locale';

import type { LandingCopy } from '../copy';
import { sampleContext } from '../sampleContext';
import { sampleLink, sampleResume } from '../sampleResume';
import HomeGlyph from './HomeGlyph.vue';

defineProps<{
  readonly copy: LandingCopy;
  readonly locale: Locale;
}>();
</script>

<template>
  <div
    class="art"
    data-testid="landing-art"
  >
    <figure
      :aria-label="copy.sampleLabel"
      class="art-figure"
      data-testid="landing-sample"
    >
      <div
        class="sheet"
        data-testid="landing-sheet"
      >
        <div class="sheet-scale">
          <ResumeDocument
            :context="sampleContext"
            :document="sampleResume"
          />
        </div>
      </div>
    </figure>
    <div
      aria-hidden="true"
      class="chips"
      data-testid="landing-chips"
    >
      <span
        class="linkpill"
        data-chip="link"
      ><span class="linkpill-icon"><HomeGlyph
        name="link"
      /></span>aboutme.vn{{ sampleLink }}</span>
      <span
        class="home-coin coin-zero"
        data-chip="coin"
      >{{ copy.hero.coin }}</span>
      <span
        class="home-coin coin-mark"
        data-chip="mark"
      ><HomeGlyph name="mark" /></span>
      <AppSeal
        class="hero-seal"
        data-chip="seal"
        :label="copy.sealLabel"
        :link="sampleLink"
        :locale="locale"
      />
    </div>
  </div>
</template>

<style scoped>
.art {
  --sheet-zoom: 0.36;
  position: relative;
  width: calc(210mm * var(--sheet-zoom));
  margin: 48px 0 -48px calc((100% - 210mm * var(--sheet-zoom)) / 2);
}

.art-figure {
  margin: 0;
}

.sheet {
  width: calc(210mm * var(--sheet-zoom));
  height: calc(297mm * var(--sheet-zoom));
  overflow: hidden;
  border-radius: 2px;
  background: var(--paper);
  box-shadow: var(--shadow-paper);
  transform: rotate(-3deg);
}

.sheet-scale {
  width: 210mm;
  height: 297mm;
  overflow: hidden;
  transform: scale(var(--sheet-zoom));
  transform-origin: 0 0;
}

.chips {
  --card: var(--paper-seal-ground);
  --seal-text: var(--paper-seal-ink);
}

.linkpill {
  position: absolute;
  z-index: 2;
  top: -32px;
  left: -5%;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 16px 9px 9px;
  border-radius: 999px;
  background: var(--chip);
  box-shadow: var(--shadow-float);
  color: var(--chip-foreground);
  font: 700 15px/1 var(--font-chrome);
  white-space: nowrap;
  transform: rotate(4deg);
}

.linkpill-icon {
  display: grid;
  flex: none;
  place-content: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--coin);
}

.linkpill-icon svg {
  width: 18px;
  height: 18px;
}

.coin-zero {
  position: absolute;
  z-index: 2;
  bottom: 25%;
  left: -5%;
  width: 64px;
  height: 64px;
  font-size: 21px;
  transform: rotate(-12deg);
}

.coin-mark {
  position: absolute;
  top: 9%;
  right: -4%;
  width: 34px;
  height: 34px;
  box-shadow:
    inset 0 -4px 0 var(--coin-edge),
    var(--shadow-float);
}

.coin-mark svg {
  width: 19px;
}

.hero-seal {
  position: absolute;
  z-index: 2;
  right: -5%;
  bottom: 13%;
  width: 164px;
  filter: drop-shadow(0 8px 8px rgba(26, 22, 80, 0.3));
}

@media (min-width: 28rem) {
  .art {
    --sheet-zoom: 0.5;
  }

  .linkpill {
    top: -36px;
    left: -7%;
    padding: 12px 20px 12px 12px;
    font-size: 18px;
  }

  .linkpill-icon {
    width: 32px;
    height: 32px;
  }

  .coin-zero {
    left: -7%;
    width: 80px;
    height: 80px;
    font-size: 26px;
  }

  .coin-mark {
    width: 40px;
    height: 40px;
  }

  .hero-seal {
    right: -7%;
    width: 200px;
  }
}

@media (min-width: 48rem) {
  .art {
    --sheet-zoom: 0.6;
    margin-top: 56px;
    margin-bottom: -64px;
  }
}

@media (min-width: 64rem) {
  .art {
    --sheet-zoom: 0.5;
    align-self: end;
    margin: 36px 0 -72px;
  }

  .linkpill {
    left: -13%;
  }

  .coin-zero {
    left: -11%;
  }

  .hero-seal {
    right: -6%;
  }
}

@media (min-width: 80rem) {
  .art {
    --sheet-zoom: 0.54;
    justify-self: center;
  }
}
</style>
