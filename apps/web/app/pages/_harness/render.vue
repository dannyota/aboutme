<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { TEMPLATES } from '@aboutme/schema/templates';
import { computed, createApp, h, onMounted, ref } from 'vue';

import fullSource from '../../../../../packages/schema/fixtures/full.json';
import vnFullSource from '../../../../../packages/schema/fixtures/vn-full.json';
import type { components } from '../../api/generated/openapi';
import JoinInvite from '../../components/public/JoinInvite.vue';
import PublicGate from '../../components/public/PublicGate.vue';
import PublicResumeApp from '../../components/public/PublicResumeApp.vue';
import ResumeDocument from '../../components/resume/ResumeDocument.vue';
import ScaledSheet from '../../components/resume/ScaledSheet.vue';
import { applyTemplate } from '../../components/resume/applyTemplate';
import type { RenderContext } from '../../components/resume/resolveRenderModel';
import {
  renderPageRule,
  useResumeStyles,
} from '../../components/resume/useResumeStyles';
import type {
  PublicGateMessage,
  PublicGateProvider,
} from '../../../server/utils/public-render/envelope';
import { GATE_STYLE } from '../../../server/workers/public-render/gate-style';
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
// The sign-in gate as the public render worker draws it
// (docs/design/viewer-analytics/sign-in-to-view.md#gate).
const isGate = !isCorpus && route.query.mode === 'gate';
const GATE_PROVIDER_SETS: ReadonlyMap<string, readonly PublicGateProvider[]>
  = new Map<string, readonly PublicGateProvider[]>([
    ['google,linkedin', ['google', 'linkedin']],
    ['google', ['google']],
    ['', []],
  ]);
const GATE_MESSAGES: ReadonlySet<string> = new Set([
  'none', 'cancelled', 'failed',
]);
let gateLng: 'en' | 'vi' = 'en';
let gateProviders: readonly PublicGateProvider[] = [];
let gateMessage: PublicGateMessage = 'none';
// The join invite the real client mounts on a sign_in resume's page.
let inviteHref: '/register' | '/login' | undefined;
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
} else if (isGate) {
  requireAllowedKeys(new Set(['lng', 'message', 'mode', 'providers']));
  const lng = singleton('lng', true);
  if (lng !== 'en' && lng !== 'vi') badQuery();
  gateLng = lng as 'en' | 'vi';
  const providers = GATE_PROVIDER_SETS.get(singleton('providers', true) ?? '');
  if (providers === undefined) badQuery();
  gateProviders = providers ?? [];
  const message = singleton('message', true) ?? '';
  if (!GATE_MESSAGES.has(message)) badQuery();
  gateMessage = message as PublicGateMessage;
  // The gate wears Be Vietnam Pro from the print-fonts sheet.
  selectedFontId = 'be-vietnam-pro';
  useHead({
    htmlAttrs: { class: 'harness-gate', lang: gateLng },
    style: [{ textContent: GATE_STYLE }],
  });
} else {
  requireAllowedKeys(
    new Set(
      [
        'align', 'fixture', 'font', 'invite', 'mode', 'paper', 'print',
        'repeat', 'scheme', 'template', 'zoom',
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
  // The harness scales its paper with the same ScaledSheet component
  // EditorPreview.vue uses for the paged preview, so a `zoom` query behaves
  // like the editor's own display scale rather than CSS `zoom`.
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
  const requestedInvite = singleton('invite');
  if (requestedInvite !== undefined) {
    if (resolvedMode !== 'public') badQuery();
    if (requestedInvite !== 'register' && requestedInvite !== 'login') {
      badQuery();
    }
    inviteHref = requestedInvite === 'register' ? '/register' : '/login';
  }

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

  const requestedRepeat = singleton('repeat');
  if (requestedRepeat !== undefined) {
    if (
      resumeDocument === undefined
      || !fixture.startsWith('sample-')
      || (requestedRepeat !== '2' && requestedRepeat !== '3')
    ) {
      badQuery();
    }
    const times = Number(requestedRepeat);
    // Repeats every section's entries to build multi-page content for the
    // print check.
    const content = resumeDocument.content as unknown as Record<
      string, { entries: Array<{ id: string }> }>;
    for (const section of Object.values(content)) {
      const base = section.entries;
      section.entries = Array.from({ length: times }, (_, copy) =>
        base.map((entry) => ({
          ...structuredClone(entry),
          id: copy === 0
            ? entry.id
            : `${entry.id.slice(0, -2)}${String(copy).padStart(2, '0')}`,
        }))).flat();
    }
  }

  const resolvedDocument = resumeDocument ?? badQuery();
  const renderLanguage: 'vi' | 'en'
    = sampleLanguage ?? (fixture === 'full' ? 'en' : 'vi');
  // A gallery sample already wears its own template, settings and all
  // (documents.ts `galleryDocument`); applying the template again here would
  // reset the one setting a sample may override, its date format
  // (docs/design/vietnam-tech-resumes.md, Dates). Filler and the full/vn-full
  // fixtures have no settings of their own, so they still take the preset.
  if (!fixture.startsWith('sample-')) {
    // The fixture owner already prints on the preset's paper; a template
    // switch keeps the owner's page format.
    resolvedDocument.customization = applyTemplate(
      {
        ...resolvedDocument.customization,
        pageFormat: template.customization.pageFormat,
      },
      template,
      resolvedDocument.content,
      renderLanguage,
    );
  }
  // Every preset prints on A4 (colors.md §4), so a screenshot cell that
  // needs Letter coverage asks for it explicitly, after the template applies.
  const requestedPaper = singleton('paper');
  if (requestedPaper !== undefined) {
    if (requestedPaper !== 'a4' && requestedPaper !== 'letter') badQuery();
    resolvedDocument.customization.pageFormat
      = requestedPaper as Resume['customization']['pageFormat'];
  }
  // Presets never set text alignment (ADR 0013), so a justify cell asks
  // for it after the template applies, with body text that wraps.
  const requestedAlign = singleton('align');
  if (requestedAlign !== undefined) {
    if (requestedAlign !== 'justify') badQuery();
    resolvedDocument.customization.font.textAlign = 'justify';
    withWrappingBody(resolvedDocument);
  }
  // A dark or Match device public page, as the owner's color scheme sets it
  // (docs/design/public-page-theme.md). Presets never set it, so a cell asks
  // for it after the template applies; only the public page shows it.
  const requestedScheme = singleton('scheme');
  if (requestedScheme !== undefined) {
    if (
      resolvedMode !== 'public'
      || (requestedScheme !== 'dark' && requestedScheme !== 'system')
    ) {
      badQuery();
    }
    resolvedDocument.customization.colorScheme
      = requestedScheme as Resume['customization']['colorScheme'];
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
    lng: renderLanguage,
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
  };
});

if (inviteHref !== undefined) {
  useHead({ htmlAttrs: { lang: context?.lng ?? 'en' } });
}

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
const harnessRoot = ref<HTMLElement>();
const inviteMounted = ref(false);

// The real client mounts the invite from the page root, outside the page's
// own app, with the document language (public-resume.client.ts).
function mountJoinInvite(href: '/register' | '/login'): void {
  const root = harnessRoot.value;
  if (root === undefined) return;
  const lng = document.documentElement.lang.split('-')[0]?.toLowerCase();
  const container = document.createElement('div');
  document.body.append(container);
  createApp({
    render: () =>
      h(JoinInvite, { lng: lng === 'vi' ? 'vi' : 'en', href, root }),
  }).mount(container);
  inviteMounted.value = true;
}

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
  if (inviteHref !== undefined) mountJoinInvite(inviteHref);
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
  <PublicGate
    v-else-if="isGate"
    data-render-mode="gate"
    :data-fonts-ready="fontsSettled ? 'true' : undefined"
    home-href="https://aboutme.vn/"
    :lng="gateLng"
    :message="gateMessage"
    page-title="Nguyễn Văn An, Backend Engineer"
    :providers="gateProviders"
    slug="harness-public"
  />
  <main
    v-else-if="
      resumeDocument !== undefined
        && context !== undefined
        && mode !== undefined
    "
    ref="harnessRoot"
    class="harness-render"
    :data-fonts-ready="fontsSettled ? 'true' : undefined"
    :data-invite-mounted="inviteMounted ? 'true' : undefined"
    :data-render-mode="mode"
  >
    <PublicResumeApp
      v-if="publicResume !== undefined"
      home-href="https://aboutme.vn/"
      :public-resume="publicResume"
    />
    <ScaledSheet
      v-else-if="requestedZoomValue !== undefined"
      :scale="requestedZoomValue"
    >
      <div
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
    </ScaledSheet>
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
/* :where() keeps the original (0,0,1) specificity per selector, so print
   and template rules that override this background still win. */
html:where(:not(.harness-gate)),
:where(html:not(.harness-gate)) body {
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
