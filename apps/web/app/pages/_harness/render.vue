<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { TEMPLATES } from '@aboutme/schema/templates';
import { computed, createApp, h, onMounted, ref } from 'vue';

import fullSource from '../../../../../packages/schema/fixtures/full.json';
import vnFullSource from '../../../../../packages/schema/fixtures/vn-full.json';
import type { components } from '../../api/generated/openapi';
import JoinInvite from '../../components/public/JoinInvite.vue';
import ProjectListPage from '../../components/public/ProjectListPage.vue';
import ProjectPage from '../../components/public/ProjectPage.vue';
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
import {
  type EntryPointFixture,
  withEntryPoints,
} from './entry-point-fixtures';
import { FIXED_PHOTO_DATA_URL, FIXED_PHOTO_SHA256 } from './photo-fixture';
import {
  ENGLISH_PRINT_FIXTURES,
  PRINT_FIXTURES,
  type PrintFixtureId,
} from './print-fixtures';
import { loadSampleFixture } from './sample-fixtures';
import { printProbe, PRINT_PROBES, type PrintProbe } from './print-probes';
import {
  projectFixture,
  projectListFixture,
  type ProjectFixture,
  type ProjectListFixture,
} from './project-fixtures';

type RenderMode = 'continuous' | 'paged' | 'public';
type FixtureId = 'full' | 'vn-full' | PrintFixtureId
  | `print-probe-${PrintProbe}`;

// vue-tsc misreads `//` in a static attribute (vuejs/language-tools#6322).
const HOME_HREF = 'https://aboutme.vn/';

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
const isProject = !isCorpus
  && (route.query.mode === 'project-list' || route.query.mode === 'project');
let projectList: ProjectListFixture | undefined;
let projectDetail: ProjectFixture | undefined;
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
let selectedProbe: PrintProbe | undefined;
let printMode = false;
let requestedZoomValue: number | undefined;
let sampleLanguage: 'vi' | 'en' | undefined;
let selectedFontId: string | undefined;
let entryPoints: EntryPointFixture | undefined;

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
  selectedFontId = 'be-vietnam-pro';
  useHead({
    htmlAttrs: { class: 'harness-gate', lang: gateLng },
    style: [{ textContent: GATE_STYLE }],
  });
} else if (isProject) {
  requireAllowedKeys(new Set(['lng', 'mode', 'scheme']));
  const lng = singleton('lng', true);
  if (lng !== 'en' && lng !== 'vi') badQuery();
  const scheme = singleton('scheme', true);
  if (scheme !== 'light' && scheme !== 'dark' && scheme !== 'system') {
    badQuery();
  }
  const pageLng = lng as 'en' | 'vi';
  const pageScheme = scheme as 'light' | 'dark' | 'system';
  if (route.query.mode === 'project-list') {
    projectList = projectListFixture(pageLng, pageScheme);
  } else {
    projectDetail = projectFixture(pageLng, pageScheme);
  }
  selectedFontId = 'be-vietnam-pro';
  useHead({
    htmlAttrs: {
      'class': 'harness-project', 'lang': pageLng, 'data-scheme': pageScheme,
    },
    link: [
      { rel: 'stylesheet', href: '/_nuxt/assets/print-fonts.css' },
      { rel: 'stylesheet', href: '/_nuxt/assets/project-pages.css' },
    ],
  });
} else {
  requireAllowedKeys(
    new Set(
      [
        'align', 'fixture', 'font', 'invite', 'links', 'lng', 'mode', 'paper',
        'print', 'repeat', 'scheme', 'template', 'zoom',
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
  const requestedPrint = singleton('print');
  if (requestedPrint !== undefined) {
    if (requestedPrint !== '1' || resolvedMode !== 'continuous') badQuery();
  }
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

  const requestedLanguage = singleton('lng');
  const probe = PRINT_PROBES.find((name) => fixture === `print-probe-${name}`);
  if (requestedLanguage !== undefined) {
    if (requestedLanguage !== 'en' && requestedLanguage !== 'vi') badQuery();
    if (PRINT_FIXTURES[fixture as PrintFixtureId] === undefined
      && probe === undefined) badQuery();
  }
  const printRecord = requestedLanguage === 'en'
    ? ENGLISH_PRINT_FIXTURES[fixture as PrintFixtureId]
    : PRINT_FIXTURES[fixture as PrintFixtureId];
  if (probe !== undefined) {
    if (resolvedMode !== 'continuous' || (templateId !== 'modern-sidebar'
      && templateId !== 'engineer-compact')) badQuery();
    selectedProbe = probe;
    sampleLanguage = requestedLanguage === 'en' ? 'en' : 'vi';
    resumeDocument = printProbe(probe, sampleLanguage,
      templateId as 'modern-sidebar' | 'engineer-compact');
    printFixture = true;
  } else if (printRecord !== undefined) {
    if (
      templateId !== 'modern-sidebar'
      || resolvedMode !== 'continuous'
    ) {
      badQuery();
    }
    resumeDocument = structuredClone(printRecord.document);
    printFixture = true;
    sampleLanguage = requestedLanguage === 'en' ? 'en' : 'vi';
  } else if (fixture === 'full') {
    resumeDocument = structuredClone(fullSource) as Resume;
  } else if (fixture === 'vn-full') {
    resumeDocument = structuredClone(vnFullSource) as Resume;
  } else {
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
  if (!fixture.startsWith('sample-') && selectedProbe === undefined) {
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
  const requestedPaper = singleton('paper');
  if (requestedPaper !== undefined) {
    if (requestedPaper !== 'a4' && requestedPaper !== 'letter') badQuery();
    resolvedDocument.customization.pageFormat
      = requestedPaper as Resume['customization']['pageFormat'];
  }
  const requestedAlign = singleton('align');
  if (requestedAlign !== undefined) {
    if (requestedAlign !== 'justify') badQuery();
    resolvedDocument.customization.font.textAlign = 'justify';
    withWrappingBody(resolvedDocument);
  }
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
  if (resolvedMode === 'public') withWrappingBody(resolvedDocument);
  const requestedLinks = singleton('links');
  if (requestedLinks !== undefined) {
    if (requestedLinks !== '1' || resolvedMode === 'paged') badQuery();
    entryPoints = withEntryPoints(resolvedDocument, renderLanguage);
  }
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
    ...(entryPoints === undefined
      ? {}
      : { projectLinks: entryPoints.projectLinks }),
  };
}

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

if (selectedProbe !== undefined
  && ['heading', 'orphan', 'widow'].includes(selectedProbe)) {
  useHead({ style: [{ textContent: '.harness-paper::before {'
    + ' content: "BOUNDARY-ANCHOR"; display: block;'
    + ' height: calc(267mm - 24px); font: 16px/24px "Be Vietnam Pro"; }' }] });
}

if (selectedProbe === 'breakable-entry') {
  useHead({ style: [{ textContent: '.harness-paper::before {'
    + ' content: "BOUNDARY-ANCHOR"; display: block;'
    + ' height: calc(267mm - 144px); font: 16px/24px "Be Vietnam Pro"; }' }] });
}

const fontsSettled = ref(false);
const harnessRoot = ref<HTMLElement>();
const inviteMounted = ref(false);

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
    :home-href="HOME_HREF"
    :lng="gateLng"
    :message="gateMessage"
    page-title="Nguyễn Văn An, Backend Engineer"
    :providers="gateProviders"
    slug="harness-public"
  />
  <ProjectListPage
    v-else-if="projectList !== undefined"
    data-render-mode="project-list"
    :data-fonts-ready="fontsSettled ? 'true' : undefined"
    :home-href="HOME_HREF"
    :projects="projectList.projects"
    :resume="projectList.resume"
  />
  <ProjectPage
    v-else-if="projectDetail !== undefined"
    data-render-mode="project"
    :data-fonts-ready="fontsSettled ? 'true' : undefined"
    :entry-link="projectDetail.entryLink"
    :home-href="HOME_HREF"
    :list-url="projectDetail.listUrl"
    :other-projects="projectDetail.otherProjects"
    :project="projectDetail.project"
    :resume="projectDetail.resume"
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
      :home-href="HOME_HREF"
      :public-resume="publicResume"
      v-bind="entryPoints ?? {}"
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

body.resume-print .harness-render,
body.resume-print .harness-paper {
  width: auto;
}

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
