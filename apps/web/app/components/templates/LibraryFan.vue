<script setup lang="ts">
const SHEETS = [
  { kind: 'columns', tilt: '-rotate-[9deg] translate-y-3.5' },
  { kind: 'plain', tilt: '-ml-[60px] -rotate-2' },
  { kind: 'band', tilt: '-ml-[60px] rotate-6 translate-y-2.5' },
] as const;
const LINES = ['w-full', 'w-11/12', 'w-4/5', 'w-full', 'w-2/3'] as const;
</script>

<template>
  <div
    aria-hidden="true"
    class="pointer-events-none absolute inset-0 overflow-clip
      rounded-[inherit]"
    data-testid="gallery-fan-layer"
  >
    <div
      class="absolute -right-10 -bottom-24 hidden lg:flex"
      data-testid="gallery-fan"
    >
      <div
        v-for="sheet in SHEETS"
        :key="sheet.kind"
        class="relative grid aspect-[210/297] w-[150px] shrink-0 content-start
          gap-1.5 overflow-hidden rounded-[var(--radius-sheet)] bg-paper p-3.5
          shadow-[var(--shadow-paper)]"
        :class="[sheet.tilt, sheet.kind === 'columns' && 'pl-14']"
        :data-fan-sheet="sheet.kind"
      >
        <span
          v-if="sheet.kind === 'band'"
          class="-mx-3.5 -mt-3.5 mb-1 block h-8 bg-paper-ink"
        />
        <span
          v-if="sheet.kind === 'columns'"
          class="absolute inset-y-0 left-0 w-[30%] bg-paper-hover"
        />
        <span class="block h-2 w-3/5 rounded-full bg-paper-ink" />
        <span class="block h-1 w-2/5 rounded-full bg-paper-muted/70" />
        <template
          v-for="section in 2"
          :key="section"
        >
          <span class="mt-1.5 block h-1 w-1/3 rounded-full bg-paper-ink/80" />
          <span
            v-for="(width, line) in LINES"
            :key="line"
            class="block h-1 rounded-full bg-paper-muted/30"
            :class="width"
          />
        </template>
      </div>
    </div>
  </div>
</template>
