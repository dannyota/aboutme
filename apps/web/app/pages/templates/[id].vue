<script setup lang="ts">
import type { SampleLanguage } from '@aboutme/schema/samples';
import { computed, ref, watch } from 'vue';

import ResumeDocument from '@/components/resume/ResumeDocument.vue';
import { Button, buttonVariants } from '@/components/ui/button';
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs';
import { locales } from '@/i18n/locale';
import { pageTitle } from '@/i18n/meta';
import { galleryCopy } from '@/i18n/templates';
import { atsText } from '@/templates/atsText';
import { galleryTemplate, sampleRole } from '@/templates/catalog';
import { galleryDocument } from '@/templates/documents';
import { templateStructuredData } from '@/templates/structuredData';

const route = useRoute();
const template = galleryTemplate(String(route.params.id));
if (template === undefined) {
  throw createError({
    statusCode: 404,
    statusMessage: 'Not found',
    fatal: true,
  });
}

const { locale } = useLocale();
const copy = computed(() => galleryCopy[locale.value]);
const detail = computed(() => copy.value.detail);
const languages = template.sampleLanguages;
const sampleLanguage = ref<SampleLanguage>(languages.includes(locale.value)
  ? locale.value
  : languages[0] ?? locale.value);
watch(locale, (next) => {
  if (languages.includes(next) || languages.length === 0) {
    sampleLanguage.value = next;
  }
});

const { data: shown } = await useAsyncData(
  () => `template-${template.id}-${sampleLanguage.value}`,
  () => galleryDocument(template, sampleLanguage.value),
  { watch: [sampleLanguage] },
);

const isSample = computed(() => shown.value?.isSample === true);
const context = computed(() => ({
  lng: sampleLanguage.value,
  mode: 'continuous' as const,
  // The info column's h1 names the template; the embedded sample's own
  // name is visual only, so the page keeps a single h1.
  nameHeading: 'p' as const,
}));
// "{name}, {role}", the role named in the sample's tag.
const persona = computed(() => [
  shown.value?.document.personalDetails.fullName,
  sampleRole(template, sampleLanguage.value, locale.value),
].filter(Boolean).join(', '));
const layout = computed(() => [
  template.columns === 1 ? detail.value.oneColumn : detail.value.twoColumns,
  template.fontName,
].join(', '));
const readingText = computed(() => shown.value === null
  || shown.value === undefined
  ? ''
  : atsText(shown.value.document, sampleLanguage.value));
const useSampleLink = computed(() =>
  `/app/new?sample=${template.id}&lng=${sampleLanguage.value}`);
const useBlankLink = `/app/new?template=${template.id}`;

useSiteSeo(() => ({
  path: `/templates/${template.id}`,
  title: pageTitle(detail.value.seoTitle(template.name)),
  description: template.purpose[locale.value],
  locale: locale.value,
}));
useHead(computed(() => ({
  script: [{
    key: 'aboutme-structured-data',
    type: 'application/ld+json',
    innerHTML: templateStructuredData(
      template,
      template.purpose[locale.value],
      locale.value,
    ),
  }],
})));
</script>

<template>
  <main
    class="template-detail mx-auto w-full max-w-7xl px-4 pb-28 pt-8
      sm:px-8 sm:pb-16 sm:pt-12"
    :data-template="template.id"
    data-testid="template-detail"
  >
    <div class="template-detail__layout">
      <aside class="template-detail__info grid content-start gap-5">
        <nav
          aria-label="Breadcrumb"
          class="text-sm text-muted-foreground"
        >
          <NuxtLink
            class="underline-offset-4 hover:underline"
            to="/templates"
          >{{ detail.breadcrumb }}</NuxtLink>
          <span aria-hidden="true"> / </span>
          <span aria-current="page">{{ template.name }}</span>
        </nav>
        <div class="grid gap-2">
          <h1 class="text-[34px] font-bold leading-tight tracking-[-0.02em]">
            {{ template.name }}
          </h1>
          <p class="text-base text-muted-foreground">
            {{ template.purpose[locale] }}
          </p>
        </div>
        <div
          v-if="languages.length > 1"
          :aria-label="detail.sampleToggle"
          class="flex w-fit rounded-md border bg-background p-0.5"
          role="group"
        >
          <Button
            v-for="lng in locales.filter((item) => languages.includes(item))"
            :key="lng"
            :aria-pressed="sampleLanguage === lng"
            class="h-8"
            :data-sample-language="lng"
            size="sm"
            type="button"
            :variant="sampleLanguage === lng ? 'default' : 'ghost'"
            @click="sampleLanguage = lng"
          >
            {{ detail.sampleLanguages[lng] }}
          </Button>
        </div>
        <dl class="grid gap-3 text-sm">
          <div v-if="isSample">
            <dt class="text-muted-foreground">
              {{ detail.sample }}
            </dt>
            <dd data-fact="sample">
              {{ persona }} {{ detail.fictional }}
            </dd>
          </div>
          <div v-if="isSample && template.samplePages !== undefined">
            <dt class="text-muted-foreground">
              {{ detail.pages }}
            </dt>
            <dd data-fact="pages">
              {{ detail.pageCount(template.samplePages) }}
            </dd>
          </div>
          <div>
            <dt class="text-muted-foreground">
              {{ detail.layout }}
            </dt>
            <dd data-fact="layout">
              {{ layout }}
            </dd>
          </div>
          <div>
            <dt class="text-muted-foreground">
              {{ detail.paper }}
            </dt>
            <dd data-fact="paper">
              {{ template.pageFormat === 'letter' ? 'Letter' : 'A4' }}
            </dd>
          </div>
          <div>
            <dt class="text-muted-foreground">
              {{ detail.photo }}
            </dt>
            <dd data-fact="photo">
              {{ template.suitsPhoto
                ? detail.suitsPhoto
                : detail.photoNotRecommended }}
            </dd>
          </div>
        </dl>
        <p
          v-if="template.columns === 2"
          class="rounded-lg bg-surface-blue p-3 text-sm"
          data-two-column-note
        >
          {{ detail.twoColumnNote[0] }}<NuxtLink
            class="text-link underline underline-offset-4"
            to="/templates/ats-plain"
          >ATS Plain</NuxtLink>{{ detail.twoColumnNote[1] }}
        </p>
        <p
          v-if="!isSample"
          class="text-sm text-muted-foreground"
          data-filler-note
        >
          {{ detail.fillerNote }}
        </p>
        <!-- On phones this block is a bar holding the primary action only. -->
        <div class="template-detail__actions grid gap-3">
          <NuxtLink
            :class="buttonVariants({ variant: 'default', size: 'lg' })"
            :data-action="isSample ? 'use-sample' : 'use-blank'"
            :to="isSample ? useSampleLink : useBlankLink"
          >
            {{ isSample ? detail.useSample : detail.useBlank }}
          </NuxtLink>
        </div>
        <p
          v-if="isSample"
          class="text-sm text-muted-foreground"
        >
          {{ detail.privateCopy }}
        </p>
        <NuxtLink
          v-if="isSample"
          class="justify-self-start text-sm text-link underline
            underline-offset-4"
          data-action="use-blank"
          :to="useBlankLink"
        >
          {{ detail.useBlank }}
        </NuxtLink>
      </aside>

      <section
        :aria-label="detail.preview"
        class="template-detail__sheet min-w-0"
      >
        <Tabs
          v-if="isSample"
          default-value="page"
        >
          <TabsList :aria-label="detail.tabsLabel">
            <TabsTrigger value="page">
              {{ detail.pageTab }}
            </TabsTrigger>
            <TabsTrigger value="ats">
              {{ detail.atsTab }}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="page">
            <div class="template-paper">
              <ResumeDocument
                v-if="shown"
                :context="context"
                :document="shown.document"
              />
            </div>
          </TabsContent>
          <TabsContent value="ats">
            <p class="mb-3 text-sm text-muted-foreground">
              {{ detail.atsHint }}
            </p>
            <pre
              class="template-paper paper-surface whitespace-pre-wrap p-6
                font-sans text-sm leading-6"
              data-ats-text
            >{{ readingText }}</pre>
          </TabsContent>
        </Tabs>
        <div
          v-else
          class="template-paper"
        >
          <ResumeDocument
            v-if="shown"
            :context="context"
            :document="shown.document"
          />
        </div>
      </section>
    </div>
  </main>
</template>

<style scoped>
.template-detail__layout {
  display: grid;
  gap: 32px;
}

.template-paper {
  overflow: hidden;
  border-radius: var(--radius-sheet);
  background: #fff;
  box-shadow: var(--shadow-paper);
}

/* Phones: the info column first, then the sheet; the primary action stays
   in a bar at the bottom of the screen. */
@media (width < 900px) {
  .template-detail__actions {
    position: fixed;
    inset: auto 0 0;
    z-index: 10;
    padding: 12px 16px calc(12px + env(safe-area-inset-bottom));
    border-top: 1px solid var(--border);
    background: var(--card);
  }
}

@media (width >= 900px) {
  .template-detail__layout {
    grid-template-columns: minmax(0, 1fr) 360px;
    gap: 48px;
  }

  .template-detail__info {
    position: sticky;
    top: 24px;
    grid-column: 2;
    grid-row: 1;
    align-self: start;
  }

  .template-detail__sheet {
    grid-column: 1;
    grid-row: 1;
  }
}
</style>
