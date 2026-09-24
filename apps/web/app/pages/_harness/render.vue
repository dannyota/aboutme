<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { TEMPLATES } from '@aboutme/schema/templates';
import { computed, onMounted, ref } from 'vue';

import fullSource from '../../../../../packages/schema/fixtures/full.json';
import vnFullSource from '../../../../../packages/schema/fixtures/vn-full.json';
import type { components } from '../../api/generated/openapi';
import PublicResumeApp from '../../components/public/PublicResumeApp.vue';
import ResumeDocument from '../../components/resume/ResumeDocument.vue';
import { applyTemplate } from '../../components/resume/applyTemplate';
import type { RenderContext } from '../../components/resume/resolveRenderModel';
import {
  renderPageRule,
  useResumeStyles,
} from '../../components/resume/useResumeStyles';
import { resolveFontSelection } from '../../utils/fontCatalog';
import { fontsReady } from '../../utils/fontsReady';
import { sanitizeRichText } from '../../utils/sanitizeRichText';
import { withWrappingBody } from './justify-fixture';
import { FIXED_PHOTO_DATA_URL, FIXED_PHOTO_SHA256 } from './photo-fixture';
import { PRINT_FIXTURES, type PrintFixtureId } from './print-fixtures';
import { loadSampleFixture } from './sample-fixtures';

type RenderMode = 'continuous' | 'paged' | 'public';
type FixtureId = 'full' | 'vn-full' | PrintFixtureId;

const route = useRoute();
const queryKeys = Object.keys(route.query);
const corpusIds = new Set(HOSTILE_CORPUS.map(({ id }) => id));
const templateById = new Map(
  TEMPLATES.map((template) => [template.id, template]),
);

const badQuery = (): never => {
  throw createError({
    statusCode: 400,
    statusMessage: 'Invalid renderer harness query.',
  });
};

function singleton(name: string, required = false): string | undefined {
  const value = route.query[name];
  if (typeof value === 'string') return value;
  if (value === undefined && !required) return undefined;
  return badQuery();
}

function requireAllowedKeys(allowed: ReadonlySet<string>): void {
  if (queryKeys.some((key) => !allowed.has(key))) badQuery();
}

const isCorpus = queryKeys.includes('payload');
const corpusHtml = ref('');
const corpusReady = ref<'raw' | 'sanitized'>();
const rawWarning = ref(false);
let corpusId: string | undefined;
let rawCorpus = false;

let resumeDocument: Resume | undefined;
let context: RenderContext | undefined;
let mode: RenderMode | undefined;
let printFixture = false;
let printMode = false;
let requestedZoomValue: number | undefined;
let sampleLanguage: 'vi' | 'en' | undefined;
let selectedFontId: string | undefined;

if (isCorpus) {
  requireAllowedKeys(new Set(['payload', 'raw']));
  corpusId = singleton('payload', true);
  if (corpusId === undefined || !corpusIds.has(corpusId)) badQuery();
  const raw = singleton('raw');
  if (raw !== undefined && raw !== '1') badQuery();
  rawCorpus = raw === '1';
} else {
  requireAllowedKeys(
    new Set(
      [
        'align', 'fixture', 'font', 'mode', 'paper', 'print', 'template',
        'zoom',
      ],
    ),
  );
  const fixture = singleton('fixture', true) as FixtureId;
  const templateId = singleton('template', true);
  const requestedMode = singleton('mode', true);
  const resolvedMode: RenderMode
    = requestedMode === 'continuous'
      ? 'continuous'
      : requestedMode === 'paged'
        ? 'paged'
        : requestedMode === 'public'
          ? 'public'
          : badQuery();
  mode = resolvedMode;
  // A non-golden fixture asks for the same print CSS the golden print
  // fixtures always carry, so a real PDF can be produced from it too.
  const requestedPrint = singleton('print');
  if (requestedPrint !== undefined) {
    if (requestedPrint !== '1' || resolvedMode !== 'continuous') badQuery();
  }
  // The paged preview's own CSS zoom (EditorPreview.vue's `.preview-sheet`)
  // is set once, before the first render, and never toggled afterward: it
  // must be requested the same way here, because PagedResume's pagination
  // measurement does not settle cleanly if the zoom changes after the fact.
  const requestedZoom = singleton('zoom');
  if (requestedZoom !== undefined) {
    if (resolvedMode !== 'paged' || !/^\d+(\.\d+)?$/u.test(requestedZoom)) {
      badQuery();
    }
    requestedZoomValue = Number(requestedZoom);
    if (
      !Number.isFinite(requestedZoomValue)
      || requestedZoomValue <= 0
      || requestedZoomValue > 4
    ) {
      badQuery();
    }
  }
  const template = templateById.get(templateId ?? '') ?? badQuery();

  const printRecord = PRINT_FIXTURES[fixture as PrintFixtureId];
  if (printRecord !== undefined) {
    if (
      templateId !== 'modern-sidebar'
      || resolvedMode !== 'continuous'
    ) {
      badQuery();
    }
    resumeDocument = structuredClone(printRecord.document);
    printFixture = true;
  } else if (fixture === 'full') {
    resumeDocument = structuredClone(fullSource) as Resume;
  } else if (fixture === 'vn-full') {
    resumeDocument = structuredClone(vnFullSource) as Resume;
  } else {
    // Gallery samples and filler: sample-<templateId>-<vi|en>, filler-<vi|en>.
    const sample = await loadSampleFixture(fixture) ?? badQuery();
    resumeDocument = sample.document;
    sampleLanguage = sample.lng;
  }
  printMode = printFixture || requestedPrint === '1';

  const resolvedDocument = resumeDocument ?? badQuery();
  // The fixture owner already prints on the preset's paper; a template switch
  // keeps the owner's page format.
  resolvedDocument.customization = applyTemplate(
    {
      ...resolvedDocument.customization,
      pageFormat: template.customization.pageFormat,
    },
    template,
    resolvedDocument.content,
  );
  // Every preset prints on A4 (colors.md §4), so a screenshot cell that
  // needs Letter coverage asks for it explicitly, after the template applies.
  const requestedPaper = singleton('paper');
  if (requestedPaper !== undefined) {
    if (requestedPaper !== 'a4' && requestedPaper !== 'letter') badQuery();
    resolvedDocument.customization.pageFormat
      = requestedPaper as Resume['customization']['pageFormat'];
  }
  // Presets never set text alignment (ADR 0041), so a justify cell asks
  // for it after the template applies, with body text that wraps.
  const requestedAlign = singleton('align');
  if (requestedAlign !== undefined) {
    if (requestedAlign !== 'justify') badQuery();
    resolvedDocument.customization.font.textAlign = 'justify';
    withWrappingBody(resolvedDocument);
  }
  // The public page measure is about line length, so its cells need body
  // text that wraps.
  if (resolvedMode === 'public') withWrappingBody(resolvedDocument);
  const requestedFont = singleton('font');
  if (requestedFont !== undefined) {
    resolveFontSelection(requestedFont);
    resolvedDocument.customization.font.family
      = requestedFont as Resume['customization']['font']['family'];
  }
  selectedFontId = resolvedDocument.customization.font.family;
  const photoUrl
    = resolvedDocument.personalDetails.photo === undefined
      ? undefined
      : await verifyFixedPhoto();
  context = {
    lng: sampleLanguage ?? (fixture === 'full' ? 'en' : 'vi'),
    mode: resolvedMode === 'public' ? 'continuous' : resolvedMode,
    ...(photoUrl === undefined ? {} : { photoUrl }),
  };
}

// The public page as the public render worker draws it, with its PDF link.
const publicResume = computed(() => {
  if (mode !== 'public' || resumeDocument === undefined) return undefined;
  const source = resumeDocument;
  const photo = source.personalDetails.photo;
  return {
    slug: 'harness-public',
    revision: '1',
    lng: context?.lng ?? 'en',
    downloadEnabled: true,
    document: {
      ...source,
      personalDetails: {
        ...source.personalDetails,
        ...(photo === undefined || context?.photoUrl === undefined
          ? {}
          : {
              photo: {
                url: context.photoUrl,
                ...(photo.crop === undefined ? {} : { crop: photo.crop }),
              },
            }),
      },
    },
  } as unknown as components['schemas']['PublicResume'];
});

async function verifyFixedPhoto(): Promise<string> {
  const prefix = 'data:image/png;base64,';
  if (!FIXED_PHOTO_DATA_URL.startsWith(prefix)) {
    throw createError({
      statusCode: 500,
      statusMessage: 'Invalid fixed photo.',
    });
  }
  const binary = atob(FIXED_PHOTO_DATA_URL.slice(prefix.length));
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  const digest = await globalThis.crypto.subtle.digest('SHA-256', bytes);
  const hash = [...new Uint8Array(digest)]
    .map((byte) => byte.toString(16).padStart(2, '0'))
    .join('');
  if (hash !== FIXED_PHOTO_SHA256) {
    throw createError({
      statusCode: 500,
      statusMessage: 'Invalid fixed photo.',
    });
  }
  return FIXED_PHOTO_DATA_URL;
}

const paperStyle = computed(() => {
  if (resumeDocument === undefined) return undefined;
  const page = useResumeStyles(resumeDocument.customization).page;
  return {
    width: `${page.widthPx}px`,
    minHeight: `${page.heightPx}px`,
    ...(requestedZoomValue === undefined
      ? {}
      : { zoom: requestedZoomValue }),
  };
});

if (printMode && resumeDocument !== undefined) {
  useHead({
    bodyAttrs: { class: 'resume-print' },
    style: [
      {
        textContent: renderPageRule(
          useResumeStyles(resumeDocument.customization).page,
        ),
      },
    ],
  });
}

const fontsSettled = ref(false);
onMounted(async () => {
  if (isCorpus) {
    const record = HOSTILE_CORPUS.find(({ id }) => id === corpusId);
    if (record === undefined) throw new Error('Unknown closed corpus ID.');
    corpusHtml.value = rawCorpus
      ? record.payload
      : sanitizeRichText(record.payload);
    rawWarning.value = rawCorpus;
    corpusReady.value = rawCorpus ? 'raw' : 'sanitized';
    return;
  }
  if (selectedFontId === undefined) return;
  const selection = resolveFontSelection(selectedFontId);
  await fontsReady(selection.id);
  await fontsReady(selection.fallbackId);
  fontsSettled.value = true;
});
</script>

<template>
  <main
    v-if="isCorpus"
    class="harness-corpus"
  >
    <p
      v-if="rawWarning"
      role="alert"
    >
      Harness-only raw CSP probe
    </p>
    <!-- The raw branch is a closed harness-only CSP probe. -->
    <!-- eslint-disable vue/no-v-html -->
    <div
      class="rich-text"
      data-corpus-mount
      :data-corpus-ready="corpusReady"
      v-html="corpusHtml"
    />
    <!-- eslint-enable vue/no-v-html -->
  </main>
  <main
    v-else-if="
      resumeDocument !== undefined
        && context !== undefined
        && mode !== undefined
    "
    class="harness-render"
    :data-fonts-ready="fontsSettled ? 'true' : undefined"
    :data-render-mode="mode"
  >
    <PublicResumeApp
      v-if="publicResume !== undefined"
      home-href="https://aboutme.vn/"
      :public-resume="publicResume"
    />
    <div
      v-else
      class="harness-paper"
      :style="printMode ? undefined : paperStyle"
    >
      <ClientOnly v-if="mode === 'paged'">
        <ResumeDocument
          :document="resumeDocument"
          :context="context"
        />
      </ClientOnly>
      <ResumeDocument
        v-else
        :document="resumeDocument"
        :context="context"
      />
    </div>
  </main>
</template>

<style>
html,
body {
  margin: 0;
  background: #d9d9d9;
}

.harness-render,
.harness-paper {
  width: fit-content;
}

.harness-paper {
  background: #fff;
}

/* A print fixture's paper spans the page. The resume is a size container,
   so a shrink-to-fit paper would collapse to zero width around it. */
body.resume-print .harness-render,
body.resume-print .harness-paper {
  width: auto;
}

/* The public page spans the viewport, like the real public <main>. */
.harness-render[data-render-mode="public"] {
  width: auto;
}

.harness-corpus {
  min-height: 100vh;
  background: #fff;
}

@media print {
  html,
  body.resume-print {
    background: #fff;
  }
}
</style>
