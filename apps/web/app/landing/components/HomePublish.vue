<script setup lang="ts">
import AppSeal from '@/components/app/AppSeal.vue';
import type { Locale } from '@/i18n/locale';

import type { LandingCopy } from '../copy';
import { sampleLink } from '../sampleResume';

defineProps<{
  readonly copy: LandingCopy;
  readonly locale: Locale;
}>();

const states = ['on', 'on', 'off'] as const;
</script>

<template>
  <section
    aria-labelledby="landing-publish-title"
    class="publish home-panel home-inverse"
    data-testid="landing-publish"
  >
    <div>
      <h2
        id="landing-publish-title"
        class="home-h2"
      >
        {{ copy.publish.title }}
      </h2>
      <p class="home-lead publish-lead">
        {{ copy.publish.lead }}
      </p>
    </div>
    <div
      class="switches"
      data-testid="landing-publish-panel"
    >
      <AppSeal
        class="publish-seal"
        data-testid="landing-publish-seal"
        :label="copy.sealLabel"
        :link="sampleLink"
        :locale="locale"
      />
      <p class="example">
        {{ copy.publish.example }}
      </p>
      <dl>
        <div
          v-for="(choice, index) in copy.publish.choices"
          :key="choice.title"
          class="row"
          :data-state="states[index]"
          data-testid="landing-publish-choice"
        >
          <dt>{{ choice.title }}</dt>
          <dd
            class="state"
            data-testid="landing-publish-state"
          >
            <span>{{ states[index] === 'on'
              ? copy.publish.stateOn
              : copy.publish.stateOff }}</span>
            <span
              aria-hidden="true"
              :class="{ on: states[index] === 'on' }"
              class="switch"
            />
          </dd>
          <dd
            v-if="index === 0"
            class="url"
          >
            aboutme.vn{{ sampleLink }}
          </dd>
          <dd class="text">
            {{ choice.text }}
          </dd>
        </div>
      </dl>
    </div>
  </section>
</template>

<style scoped>
.publish {
  display: grid;
  gap: 64px;
  background: var(--panel);
  color: var(--panel-foreground);
}

.publish-lead {
  color: var(--panel-muted);
}

.switches {
  position: relative;
  padding: 16px 20px 8px;
  border-radius: var(--radius-card);
  background: var(--card);
  color: var(--foreground);
}

.publish-seal {
  position: absolute;
  top: -50px;
  right: -6px;
  width: 168px;
  filter: drop-shadow(0 10px 10px rgba(0, 0, 0, 0.35));
}

.example {
  color: var(--muted-foreground);
  font-size: 14px;
  font-weight: 600;
}

.row {
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: center;
  gap: 4px 18px;
  padding: 18px 0;
  border-top: 1px solid var(--hairline);
}

.row:first-child {
  border-top: 0;
}

.row dt {
  font-size: 16px;
  font-weight: 800;
  line-height: 1.4;
}

.row .text {
  grid-column: 1;
  color: var(--muted-foreground);
  font-size: 15px;
  line-height: 1.5;
}

.row .url {
  grid-column: 1;
  font-size: 15px;
  font-weight: 700;
  line-height: 1.5;
}

.state {
  display: flex;
  grid-row: 1 / span 2;
  grid-column: 2;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 700;
}

.row:first-child .state {
  grid-row: 1 / span 3;
}

.switch {
  position: relative;
  flex: none;
  width: 52px;
  height: 30px;
  border-radius: 999px;
  background: var(--control);
}

.switch::after {
  content: "";
  position: absolute;
  top: 4px;
  left: 4px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--chip);
  box-shadow: 0 1px 3px rgba(26, 22, 80, 0.35);
}

.switch.on {
  background: var(--brand);
}

.switch.on::after {
  left: 26px;
}

@media (min-width: 28rem) {
  .publish-seal {
    top: -58px;
    right: -18px;
    width: 200px;
  }
}

@media (min-width: 48rem) {
  .switches {
    padding: 22px 30px 8px;
  }

  .publish-seal {
    right: -22px;
  }
}

@media (min-width: 64rem) {
  .publish {
    grid-template-columns: minmax(0, 5fr) minmax(0, 6fr);
    align-items: center;
    gap: 56px;
  }
}

@media (min-width: 80rem) {
  .publish {
    gap: 64px;
  }
}
</style>
