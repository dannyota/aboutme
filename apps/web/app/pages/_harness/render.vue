<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { TEMPLATES } from '@aboutme/schema/templates';
import { computed, onMounted, ref } from 'vue';

import fullSource from '../../../../../packages/schema/fixtures/full.json';
import vnFullSource from '../../../../../packages/schema/fixtures/vn-full.json';
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
import { FIXED_PHOTO_DATA_URL, FIXED_PHOTO_SHA256 } from './photo-fixture';
import { PRINT_FIXTURES, type PrintFixtureId } from './print-fixtures';

type RenderMode = 'continuous' | 'paged';
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
let selectedFontId: string | undefined;

if (isCorpus) {
  requireAllowedKeys(new Set(['payload', 'raw']));
  corpusId = singleton('payload', true);
  if (corpusId === undefined || !corpusIds.has(corpusId)) badQuery();
  const raw = singleton('raw');
  if (raw !== undefined && raw !== '1') badQuery();
  rawCorpus = raw === '1';
} else {
  requireAllowedKeys(new Set(['fixture', 'font', 'mode', 'template']));
  const fixture = singleton('fixture', true) as FixtureId;
  const templateId = singleton('template', true);
  const requestedMode = singleton('mode', true);
  const resolvedMode: RenderMode
    = requestedMode === 'continuous'
      ? 'continuous'
      : requestedMode === 'paged'
        ? 'paged'
        : badQuery();
  mode = resolvedMode;
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
    badQuery();
  }

  const resolvedDocument = resumeDocument ?? badQuery();
  resolvedDocument.customization = applyTemplate(
    resolvedDocument.customization,
    template,
    resolvedDocument.content,
  );
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
    lng: fixture === 'full' ? 'en' : 'vi',
    mode: resolvedMode,
    ...(photoUrl === undefined ? {} : { photoUrl }),
  };
}

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
  };
});

if (printFixture && resumeDocument !== undefined) {
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
    <div
      class="harness-paper"
      :style="paperStyle"
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

.harness-corpus {
  min-height: 100vh;
  background: #fff;
}
</style>
