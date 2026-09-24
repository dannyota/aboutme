<script setup lang="ts">
import { ArrowRight, FileDown, Link2, Lock, Sparkles } from '@lucide/vue';
import { computed } from 'vue';

import AppSeal from '@/components/app/AppSeal.vue';
import ResumeDocument from '@/components/resume/ResumeDocument.vue';
import TemplateCard from '@/components/templates/TemplateCard.vue';
import { buttonVariants } from '@/components/ui/button';
import { legalCopy } from '@/i18n/legal';
import { homeTitle } from '@/i18n/meta';
import { galleryCopy } from '@/i18n/templates';
import { landingCopy } from '@/landing/copy';
import { sampleContext } from '@/landing/sampleContext';
import { sampleLink, sampleResume } from '@/landing/sampleResume';
import { SHOWCASE } from '@/landing/showcase';
import { homeStructuredData } from '@/landing/structuredData';
import { cn } from '@/lib/utils';
import { galleryTemplate } from '@/templates/catalog';

const { authState } = useAuth();
const signedIn = computed(() => authState.value === 'authenticated');
const { locale } = useLocale();
const copy = computed(() => landingCopy[locale.value]);
const legal = computed(() => legalCopy[locale.value]);
const gallery = computed(() => galleryCopy[locale.value]);
// The head of the second headline line, with the emphasized suffix removed
// (DESIGN.md; ADR 0050): "Your link. " before "Your control." lights up.
const head = computed(() => copy.value.title[1].slice(
  0,
  copy.value.title[1].length - copy.value.titleEmphasis.length,
));
const publishStates = ['on', 'on', 'off'] as const;

useSiteSeo(() => ({
  path: '/',
  title: homeTitle[locale.value],
  description: copy.value.description,
  locale: locale.value,
}));
useHead(computed(() => ({
  script: [{
    key: 'aboutme-structured-data',
    type: 'application/ld+json',
    innerHTML: homeStructuredData(copy.value.description),
  }],
})));
</script>

<template>
  <main
    class="landing mx-auto w-full max-w-7xl overflow-x-clip px-4 pb-10
      sm:px-8 min-[42rem]:pb-12"
    data-testid="landing"
  >
    <section
      aria-labelledby="landing-title"
      class="grid gap-12 pt-10 min-[42rem]:pt-16 lg:grid-cols-12
        lg:items-center lg:gap-8 lg:pt-20"
      data-testid="landing-hero"
    >
      <div class="max-w-2xl lg:col-span-5 lg:max-w-none">
        <h1
          id="landing-title"
          class="text-balance text-4xl font-bold tracking-tight lg:text-6xl"
          data-testid="landing-title"
        >
          <span class="block">{{ copy.title[0] }}</span>{{ ' ' }}<span
            class="block"
          >{{ head }}<span
            class="bg-(image:--gradient-brand) bg-clip-text text-transparent
              box-decoration-clone forced-colors:bg-none
              forced-colors:text-[color:CanvasText]"
            data-testid="landing-title-emphasis"
          >{{ copy.titleEmphasis }}</span></span>
        </h1>
        <p
          class="landing-lead mt-6 max-w-xl text-md leading-relaxed
            text-muted-foreground lg:text-lg"
        >
          {{ copy.lead }}
        </p>
        <div
          v-if="!signedIn"
          class="mt-8 flex flex-col gap-3 min-[28rem]:flex-row
            min-[28rem]:flex-wrap min-[28rem]:items-center"
        >
          <NuxtLink
            :class="cn(
              buttonVariants({ size: 'lg' }),
              'h-12 rounded-lg px-6 text-md font-semibold',
              'text-primary-foreground bg-(image:--gradient-brand)',
              'shadow-[var(--shadow-cta)]',
              'hover:bg-(image:--gradient-brand-strong)',
            )"
            data-testid="landing-create-account"
            to="/register"
          >{{ copy.createResume }}</NuxtLink>
          <NuxtLink
            :class="cn(
              buttonVariants({ variant: 'outline', size: 'lg' }),
              'h-12 rounded-lg bg-card px-6 text-md font-semibold',
            )"
            data-testid="landing-browse-templates"
            to="/templates"
          >{{ copy.browseTemplates }}</NuxtLink>
          <NuxtLink
            class="inline-flex h-10 items-center self-start px-1 text-md
              font-medium text-link underline-offset-4 hover:underline
              min-[28rem]:self-auto"
            data-testid="landing-sign-in"
            to="/login"
          >{{ copy.signIn }}</NuxtLink>
        </div>
        <div
          v-else
          class="mt-8 flex flex-col gap-3 min-[28rem]:flex-row
            min-[28rem]:flex-wrap min-[28rem]:items-center"
        >
          <NuxtLink
            :class="cn(
              buttonVariants({ size: 'lg' }),
              'h-12 rounded-lg px-6 text-md font-semibold',
              'text-primary-foreground bg-(image:--gradient-brand)',
              'shadow-[var(--shadow-cta)]',
              'hover:bg-(image:--gradient-brand-strong)',
            )"
            data-testid="landing-open-resumes"
            to="/app/resumes"
          >{{ copy.openResumes }}</NuxtLink>
          <NuxtLink
            :class="cn(
              buttonVariants({ variant: 'outline', size: 'lg' }),
              'h-12 rounded-lg bg-card px-6 text-md font-semibold',
            )"
            data-testid="landing-browse-templates"
            to="/templates"
          >{{ copy.browseTemplates }}</NuxtLink>
        </div>
      </div>

      <figure
        :aria-label="copy.sampleLabel"
        class="relative mx-auto w-fit max-w-full lg:col-span-7"
        data-testid="landing-sample"
      >
        <div class="relative isolate">
          <div
            aria-hidden="true"
            class="pointer-events-none absolute -inset-x-4 -inset-y-8 -z-10
              bg-(image:--gradient-hero-glow) min-[42rem]:-inset-10"
            data-testid="landing-glow"
          />
          <div
            aria-hidden="true"
            class="absolute inset-0 -z-10 translate-x-3 -translate-y-3
              rounded-[var(--radius-sheet)] border border-border
              bg-white/70 shadow-[var(--shadow-paper)] dark:bg-white/10
              min-[42rem]:translate-x-6 min-[42rem]:-translate-y-6
              min-[42rem]:rotate-2"
            data-testid="landing-ghost"
          />
          <div
            class="landing-sheet rounded-[var(--radius-sheet)] bg-white
              shadow-[var(--shadow-paper)]"
            data-testid="landing-sheet"
          >
            <ResumeDocument
              :context="sampleContext"
              :document="sampleResume"
            />
          </div>
          <AppSeal
            :label="copy.sealLabel"
            :link="sampleLink"
            class="landing-seal"
            data-testid="landing-seal"
            size="stamp"
          />
        </div>
        <ul
          aria-hidden="true"
          class="mt-6 flex flex-wrap justify-center gap-2
            min-[42rem]:contents"
          data-testid="landing-chips"
        >
          <li
            class="inline-flex h-9 items-center gap-2 whitespace-nowrap
              rounded-full border border-border bg-card px-4 text-sm
              font-medium text-foreground shadow-[var(--shadow-product)]
              min-[42rem]:absolute min-[42rem]:-left-16 min-[42rem]:top-4"
            data-chip="private"
          >
            <Lock
              aria-hidden="true"
              class="size-4 shrink-0 text-brand-blue"
            />{{ copy.heroChips.private }}
          </li>
          <li
            class="inline-flex h-9 items-center gap-2 whitespace-nowrap
              rounded-full border border-border bg-card px-4 text-sm
              font-medium text-foreground shadow-[var(--shadow-product)]
              min-[42rem]:absolute min-[42rem]:-right-10
              min-[42rem]:-top-4"
            data-chip="pdf"
          >
            <FileDown
              aria-hidden="true"
              class="size-4 shrink-0 text-brand-indigo"
            />{{ copy.heroChips.pdf }}
          </li>
          <li
            class="inline-flex h-9 items-center gap-2 whitespace-nowrap
              rounded-full border border-border bg-card px-4 text-sm
              font-medium text-foreground shadow-[var(--shadow-product)]
              min-[42rem]:absolute min-[42rem]:-bottom-4
              min-[42rem]:left-10"
            data-chip="link"
          >
            <Link2
              aria-hidden="true"
              class="size-4 shrink-0 text-brand-purple"
            />aboutme.vn{{ sampleLink }}
          </li>
        </ul>
      </figure>
    </section>

    <section
      aria-labelledby="landing-principles-title"
      class="mt-20 lg:mt-28"
      data-testid="landing-principles"
    >
      <h2
        id="landing-principles-title"
        class="sr-only"
      >
        {{ copy.principlesTitle }}
      </h2>
      <ul class="grid gap-4 lg:grid-cols-3 lg:gap-6">
        <li
          v-for="(point, index) in copy.points"
          :key="point.title"
          :class="cn(
            'rounded-[var(--radius-feature)] border border-border p-6',
            'shadow-[var(--shadow-product)] min-[42rem]:flex',
            'min-[42rem]:gap-6 lg:block lg:p-8',
            ['bg-surface-blue', 'bg-surface-indigo', 'bg-surface-pink'][
              index
            ],
          )"
          data-testid="landing-point"
        >
          <span
            aria-hidden="true"
            class="flex size-12 shrink-0 items-center justify-center
              rounded-xl border border-border bg-card"
          >
            <Lock
              v-if="index === 0"
              class="size-6 text-brand-blue"
            />
            <Link2
              v-else-if="index === 1"
              class="size-6 text-brand-indigo"
            />
            <Sparkles
              v-else
              class="size-6 text-brand-purple"
            />
          </span>
          <strong
            class="mt-6 block text-lg font-semibold min-[42rem]:mt-0
              lg:mt-6"
            data-testid="landing-point-title"
          >{{ point.title }}</strong>
          <p class="mt-2 text-md leading-relaxed text-muted-foreground">
            {{ point.text }}
          </p>
        </li>
      </ul>
    </section>

    <section
      aria-labelledby="landing-templates-title"
      class="mt-20 lg:mt-28"
      data-testid="landing-templates"
    >
      <div
        class="flex flex-col gap-4 min-[42rem]:flex-row
          min-[42rem]:items-end min-[42rem]:justify-between"
      >
        <div class="max-w-2xl">
          <h2
            id="landing-templates-title"
            class="text-2xl font-bold tracking-tight lg:text-4xl"
          >
            {{ copy.templatesTitle }}
          </h2>
          <p class="mt-3 text-md leading-relaxed text-muted-foreground">
            {{ copy.templatesLead }}
          </p>
        </div>
        <NuxtLink
          class="inline-flex items-center gap-1.5 self-start text-md
            font-medium text-link underline-offset-4 hover:underline
            min-[42rem]:self-auto"
          data-testid="landing-browse-all"
          to="/templates"
        >{{ copy.browseAllTemplates }}<ArrowRight
          aria-hidden="true"
          class="size-4"
        /></NuxtLink>
      </div>
      <nav
        :aria-label="copy.templateCategoriesLabel"
        class="mt-6"
      >
        <ul class="flex flex-wrap gap-2">
          <li
            v-for="entry in SHOWCASE"
            :key="entry.filter"
          >
            <NuxtLink
              :data-filter="entry.filter"
              :to="{ path: '/templates', query: { filter: entry.filter } }"
              class="inline-flex h-9 items-center rounded-full border
                border-border bg-card px-4 text-sm font-medium
                text-foreground transition-colors hover:bg-surface-indigo"
              data-testid="landing-template-filter"
            >{{ gallery.filters[entry.filter] }}</NuxtLink>
          </li>
        </ul>
      </nav>
      <ul
        class="mt-8 grid grid-cols-2 gap-x-4 gap-y-8 lg:grid-cols-4
          lg:gap-x-6"
        data-testid="landing-template-grid"
      >
        <li
          v-for="entry in SHOWCASE"
          :key="entry.id"
        >
          <TemplateCard
            :illustrative="gallery.illustrative"
            :locale="locale"
            :template="galleryTemplate(entry.id)!"
          />
        </li>
      </ul>
    </section>

    <section
      aria-labelledby="landing-publish-title"
      class="mt-20 grid gap-10 lg:mt-28 lg:grid-cols-12 lg:items-center
        lg:gap-8"
      data-testid="landing-publish"
    >
      <div class="lg:col-span-5">
        <h2
          id="landing-publish-title"
          class="text-2xl font-bold tracking-tight lg:text-4xl"
        >
          {{ copy.publishTitle }}
        </h2>
        <p
          class="mt-4 max-w-xl text-md leading-relaxed text-muted-foreground
            lg:text-lg"
        >
          {{ copy.publishLead }}
        </p>
      </div>
      <div
        class="relative mx-auto w-full max-w-xl lg:col-span-6 lg:col-start-7
          lg:mx-0 lg:max-w-none"
        data-testid="landing-publish-panel"
      >
        <div
          class="rounded-[var(--radius-feature)] border border-border
            bg-card p-6 shadow-[var(--shadow-product)] min-[42rem]:p-8"
        >
          <p class="text-sm font-medium text-muted-foreground">
            {{ copy.publishExample }}
          </p>
          <dl class="mt-4 divide-y divide-border">
            <div
              v-for="(choice, index) in copy.publishChoices"
              :key="choice.title"
              :data-state="publishStates[index]"
              class="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 py-5
                last:pb-0"
              data-testid="landing-publish-choice"
            >
              <dt class="text-md font-semibold">
                {{ choice.title }}
              </dt>
              <dd
                class="col-start-2 row-start-1 flex items-center gap-2
                  text-sm font-medium"
                data-testid="landing-publish-state"
              >
                <span
                  v-if="publishStates[index] === 'on'"
                  class="text-foreground"
                >{{ copy.stateOn }}</span>
                <span
                  v-else
                  class="text-muted-foreground"
                >{{ copy.stateOff }}</span>
                <span
                  :class="publishStates[index] === 'on'
                    ? 'bg-primary'
                    : 'bg-input'"
                  aria-hidden="true"
                  class="relative inline-flex h-5 w-9 shrink-0 rounded-full"
                >
                  <span
                    :class="publishStates[index] === 'on'
                      ? 'translate-x-4 bg-primary-foreground'
                      : 'bg-card'"
                    class="absolute left-0.5 top-0.5 size-4 rounded-full
                      shadow-xs"
                  />
                </span>
              </dd>
              <dd
                v-if="index === 0"
                class="text-sm font-medium text-foreground"
              >aboutme.vn{{ sampleLink }}</dd>
              <dd class="text-sm text-muted-foreground">
                {{ choice.text }}
              </dd>
            </div>
          </dl>
        </div>
        <AppSeal
          :label="copy.sealLabel"
          :link="sampleLink"
          class="absolute -top-12 right-4 min-[42rem]:-right-6"
          data-testid="landing-publish-seal"
          size="stamp"
        />
      </div>
    </section>

    <section
      aria-labelledby="landing-source-title"
      class="mt-20 border-t border-border pt-12 lg:mt-28 lg:flex
        lg:items-center lg:justify-between lg:gap-8 min-[42rem]:pt-16"
      data-testid="landing-license"
    >
      <div class="max-w-2xl">
        <h2
          id="landing-source-title"
          class="text-2xl font-bold tracking-tight lg:text-4xl"
        >
          {{ copy.openSourceTitle }}
        </h2>
        <p class="mt-3 text-md leading-relaxed text-muted-foreground">
          {{ copy.openSourceText }} {{ copy.licensePrefix }} <a
            class="font-medium text-link underline underline-offset-4"
            data-testid="landing-license-link"
            href="https://github.com/dannyota/aboutme"
            rel="noopener noreferrer"
          >AGPL-3.0</a>.
        </p>
      </div>
      <a
        :class="cn(
          buttonVariants({ variant: 'outline', size: 'lg' }),
          'mt-6 h-12 rounded-lg bg-card px-6 text-md font-semibold',
          'lg:mt-0',
        )"
        data-testid="landing-source-link"
        href="https://github.com/dannyota/aboutme"
        rel="noopener noreferrer"
      >{{ copy.viewSource }}</a>
    </section>

    <footer
      class="mt-12 flex flex-wrap items-center gap-x-4 gap-y-2 border-t
        border-border pt-6 text-sm text-muted-foreground"
      data-testid="landing-footer"
    >
      <span>aboutme.vn</span>
      <NuxtLink
        class="text-link underline-offset-4 hover:underline"
        data-testid="landing-terms-link"
        to="/terms"
      >{{ legal.termsLink }}</NuxtLink>
      <NuxtLink
        class="text-link underline-offset-4 hover:underline"
        data-testid="landing-privacy-link"
        to="/privacy"
      >{{ legal.privacyLink }}</NuxtLink>
    </footer>
  </main>
</template>

<style scoped>
.landing-sheet {
  width: 210mm;
  min-height: 297mm;
  overflow: hidden;
  zoom: 0.39;
}

@media (min-width: 28rem) {
  .landing-sheet {
    zoom: 0.5;
  }
}

@media (min-width: 42rem) {
  .landing-sheet {
    zoom: 0.6;
  }
}

@media (min-width: 80rem) {
  .landing-sheet {
    zoom: 0.64;
  }
}

.landing-seal {
  position: absolute;
  left: 30%;
  bottom: 16px;
}

@media (min-width: 42rem) {
  .landing-seal {
    left: auto;
    right: -32px;
    bottom: 24px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .landing {
    scroll-behavior: auto;
  }
}
</style>
