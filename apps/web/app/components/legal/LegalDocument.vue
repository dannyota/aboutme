<script setup lang="ts">
import {
  type LegalDocument,
  legalContact,
  repositoryUrl,
} from '@/i18n/legal';

defineProps<{
  document: LegalDocument;
  updated: string;
  testid: string;
}>();
</script>

<template>
  <main
    class="mx-auto box-content max-w-[44rem] px-4 py-16 md:px-6"
    :data-testid="testid"
  >
    <h1
      class="border-b pb-4 text-2xl font-extrabold tracking-[-0.02em]"
      data-page-title
    >
      {{ document.title }}
    </h1>
    <p
      class="mt-4 text-sm text-muted-foreground"
      data-testid="legal-updated"
    >
      {{ updated }}
    </p>
    <p class="mt-6 text-md">
      {{ document.intro }}
    </p>
    <p
      v-if="document.operator"
      class="mt-3 text-md"
      data-testid="legal-operator"
    >
      {{ document.operator.text }} {{ document.operator.contactLabel }}:
      <a
        class="text-link underline decoration-1 underline-offset-4"
        :href="`mailto:${legalContact}`"
      >{{ legalContact }}</a>.
    </p>
    <section
      v-for="section in document.sections"
      :key="section.heading"
      class="mt-10 border-t border-border pt-6"
    >
      <h2 class="text-xl font-extrabold tracking-[-0.01em]">
        {{ section.heading }}
      </h2>
      <p
        v-for="paragraph in section.paragraphs ?? []"
        :key="paragraph"
        class="mt-3 text-md"
      >
        {{ paragraph }}
      </p>
      <ul
        v-if="section.items"
        class="mt-3 grid list-disc gap-2 pl-5 text-md"
      >
        <li
          v-for="item in section.items"
          :key="item"
        >
          {{ item }}
        </li>
      </ul>
      <p
        v-for="paragraph in section.after ?? []"
        :key="paragraph"
        class="mt-3 text-md"
      >
        {{ paragraph }}
      </p>
      <p
        v-if="section.repositoryLink"
        class="mt-3 text-md"
      >
        <a
          class="text-link underline decoration-1 underline-offset-4"
          :href="repositoryUrl"
          rel="noopener noreferrer"
        >{{ section.repositoryLink }}</a>
      </p>
      <p
        v-if="section.contactLabel"
        class="mt-3 text-md"
      >
        {{ section.contactLabel }}:
        <a
          class="text-link underline decoration-1 underline-offset-4"
          data-testid="legal-contact"
          :href="`mailto:${legalContact}`"
        >{{ legalContact }}</a>
      </p>
    </section>
  </main>
</template>
