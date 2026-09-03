<script setup lang="ts">
import {
  FileText,
  Image,
  LayoutList,
  Palette,
  PanelsTopLeft,
  Settings2,
  Sparkles,
  UserRound,
} from '@lucide/vue';
import { computed, nextTick, ref } from 'vue';

import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import type { SaveState } from '../../editor/types';
import type { ResumeRecord } from '../../stores/resumes';
import AccountControl from '../ui/AccountControl.vue';
import ThemeToggle from '../ui/ThemeToggle.vue';
import CustomizationPanel from './customization/CustomizationPanel.vue';
import PersonalDetailsPanel from './forms/PersonalDetailsPanel.vue';
import SectionPanel from './forms/SectionPanel.vue';
import PhotoPanel from './photo/PhotoPanel.vue';
import StructurePanel from './structure/StructurePanel.vue';
import TemplatePanel from './templates/TemplatePanel.vue';
import ConflictPanel from './ConflictPanel.vue';
import EditorPreview from './EditorPreview.vue';
import ErrorSummary from './ErrorSummary.vue';
import SaveStatus from './SaveStatus.vue';
import PublishDialog from './PublishDialog.vue';
import '../../assets/css/editor.css';

type InspectorPanel
  = | { readonly kind: 'personal' }
    | { readonly kind: 'section'; readonly key: string }
    | { readonly kind: 'structure' }
    | { readonly kind: 'customization' }
    | { readonly kind: 'templates' }
    | { readonly kind: 'photo' };

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
}>();

const inspector = ref<InspectorPanel>({ kind: 'personal' });
const narrowRegion = ref<'editor' | 'preview'>('editor');
const publishOpen = ref(false);
const document = computed(() => props.record.current.document);
const placement = computed(() => document.value.customization.layout.sections);
const outline = computed(() => [
  { key: 'personal', label: 'Personal details' },
  ...[...placement.value.main, ...placement.value.sidebar].flatMap((key) => {
    const section = document.value.content[key];
    return section === undefined
      ? []
      : [
          {
            key,
            label: section.displayName ?? sectionLabel(section.sectionType),
          },
        ];
  }),
]);
const selectedSection = computed(() =>
  inspector.value.kind === 'section'
    ? document.value.content[inspector.value.key]
    : undefined,
);
const photoUrl = computed(() => {
  const read = props.record.photoRead;
  const key = document.value.personalDetails.photo?.key;
  return read.kind === 'ready' && read.binding === key
    ? read.dataUrl
    : undefined;
});
const issues = computed(() => Object.values(props.record.issues).flat());
const saveState = computed<SaveState>(() => {
  const record = props.record;
  if (record.sessionLost) return 'session-lost';
  if (record.conflicts.length > 0) return 'conflict';
  if (record.attempt?.kind === 'unknown') {
    return record.attempt.reason === 'transport' ? 'offline' : 'error';
  }
  if (
    record.attempt?.kind === 'failed'
    || record.attempt?.kind === 'retry-later'
    || record.templateState?.kind === 'partial'
    || record.opaquePhotoOutcome !== null
    || issues.value.length > 0
  ) {
    return 'error';
  }
  if (record.attempt?.kind === 'dispatching') return 'saving';
  if (record.pending.length > 0 || record.completeReadRequired) return 'dirty';
  return 'saved';
});

function selectOutline(key: string): void {
  inspector.value
    = key === 'personal' ? { kind: 'personal' } : { kind: 'section', key };
}

async function focusIssue(path: string): Promise<void> {
  const sectionMatch = path.match(/(?:content[./])([^./]+)/);
  if (sectionMatch?.[1] !== undefined) {
    inspector.value = { kind: 'section', key: sectionMatch[1] };
  } else if (path.includes('customization')) {
    inspector.value = { kind: 'customization' };
  } else {
    inspector.value = { kind: 'personal' };
  }
  await nextTick();
  [...globalThis.document.querySelectorAll<HTMLElement>('[data-issue]')]
    .find((element) => element.dataset.issue === path)
    ?.click();
}

function openInspector(
  target:
    | { readonly kind: 'section'; readonly key: string }
    | { readonly kind: 'structure' | 'templates' | 'photo' },
): void {
  inspector.value = target;
}

async function discardAndSignIn(): Promise<void> {
  props.actions.discard();
  await navigateTo('/login');
}

function sectionLabel(type: string): string {
  const labels: Readonly<Record<string, string>> = {
    profile: 'Summary',
    work: 'Experience',
    education: 'Education',
    skill: 'Skills',
    language: 'Languages',
    certificate: 'Certificates',
    project: 'Projects',
    custom: 'Custom section',
  };
  return labels[type] ?? 'Section';
}
</script>

<template>
  <main class="editor-shell">
    <header class="editor-topbar">
      <NuxtLink
        class="editor-brand"
        to="/app/resumes"
      > aboutme </NuxtLink>
      <div class="editor-title-group">
        <h1 data-resume-title>
          {{ record.current.metadata.title }}
        </h1>
        <SaveStatus :state="saveState" />
      </div>
      <button
        type="button"
        class="editor-publish-action"
        data-action="publish"
        @click="publishOpen = true"
      >
        Publish
      </button>
      <div
        class="editor-view-switcher"
        aria-label="Editor view"
      >
        <button
          type="button"
          data-action="show-editor"
          :aria-pressed="narrowRegion === 'editor'"
          @click="narrowRegion = 'editor'"
        >
          Editor
        </button>
        <button
          type="button"
          data-action="show-preview"
          :aria-pressed="narrowRegion === 'preview'"
          @click="narrowRegion = 'preview'"
        >
          Preview
        </button>
      </div>
      <div class="editor-account-actions">
        <AccountControl />
        <ThemeToggle />
      </div>
    </header>

    <PublishDialog
      :open="publishOpen"
      :actions="actions"
      :record="record"
      @close="publishOpen = false"
      @focus-issue="focusIssue"
    />

    <section
      v-if="record.sessionLost"
      class="editor-session-lost"
      aria-labelledby="editor-session-title"
      role="alert"
    >
      <h2 id="editor-session-title">
        Sign in to continue editing
      </h2>
      <p>Your unsaved work is still open in this tab.</p>
      <a
        href="/login"
        target="_blank"
        rel="noopener noreferrer"
      >
        Open sign-in in another tab
      </a>
      <button
        type="button"
        data-action="resume-after-auth"
        @click="actions.resumeAfterAuth()"
      >
        Resume after sign-in
      </button>
      <button
        type="button"
        @click="discardAndSignIn"
      >
        Discard and sign in
      </button>
    </section>

    <aside
      class="editor-app-rail"
      data-region="app-rail"
      aria-label="Editor tools"
    >
      <button
        type="button"
        aria-label="Document"
        :aria-pressed="
          inspector.kind === 'personal' || inspector.kind === 'section'
        "
        @click="inspector = { kind: 'personal' }"
      >
        <FileText
          :size="20"
          aria-hidden="true"
        />
      </button>
      <button
        type="button"
        aria-label="Structure"
        :aria-pressed="inspector.kind === 'structure'"
        @click="inspector = { kind: 'structure' }"
      >
        <LayoutList
          :size="20"
          aria-hidden="true"
        />
      </button>
      <button
        type="button"
        aria-label="Design"
        :aria-pressed="inspector.kind === 'customization'"
        @click="inspector = { kind: 'customization' }"
      >
        <Palette
          :size="20"
          aria-hidden="true"
        />
      </button>
      <button
        type="button"
        aria-label="Templates"
        :aria-pressed="inspector.kind === 'templates'"
        @click="inspector = { kind: 'templates' }"
      >
        <Sparkles
          :size="20"
          aria-hidden="true"
        />
      </button>
      <button
        type="button"
        aria-label="Photo"
        :aria-pressed="inspector.kind === 'photo'"
        @click="inspector = { kind: 'photo' }"
      >
        <Image
          :size="20"
          aria-hidden="true"
        />
      </button>
      <span class="editor-app-rail__spacer" />
      <NuxtLink
        class="editor-rail-link"
        to="/app/settings/sessions"
        aria-label="Account settings"
      >
        <Settings2
          :size="20"
          aria-hidden="true"
        />
      </NuxtLink>
      <span
        class="editor-rail-avatar"
        aria-hidden="true"
      >
        <UserRound :size="18" />
      </span>
    </aside>

    <aside
      class="editor-outline"
      data-region="outline"
      data-responsive-region="editor"
      :data-narrow-active="narrowRegion === 'editor'"
    >
      <div class="editor-panel-heading">
        <h2>Resume</h2>
        <button
          type="button"
          aria-label="Add section"
          @click="inspector = { kind: 'structure' }"
        >
          +
        </button>
      </div>
      <nav aria-label="Resume outline">
        <button
          v-for="item in outline"
          :key="item.key"
          type="button"
          :data-outline-key="item.key"
          :aria-current="
            (inspector.kind === 'section' && inspector.key === item.key)
              || (inspector.kind === 'personal' && item.key === 'personal')
              ? 'page'
              : undefined
          "
          @click="selectOutline(item.key)"
        >
          <PanelsTopLeft
            :size="18"
            aria-hidden="true"
          />
          <span>{{ item.label }}</span>
        </button>
      </nav>
      <button
        class="editor-add-section"
        type="button"
        @click="inspector = { kind: 'structure' }"
      >
        + Add section
      </button>
    </aside>

    <div
      class="editor-preview-region"
      data-region="preview"
      data-responsive-region="preview"
      :data-narrow-active="narrowRegion === 'preview'"
    >
      <EditorPreview
        :document="document"
        :lng="record.current.metadata.lng"
        :photo-url="photoUrl"
      />
    </div>

    <aside
      class="editor-inspector"
      data-region="inspector"
      data-responsive-region="editor"
      :data-narrow-active="narrowRegion === 'editor'"
    >
      <ErrorSummary
        :issues="issues"
        @focus-issue="focusIssue"
      />
      <ConflictPanel
        :actions="actions"
        :conflicts="record.conflicts"
        @open-inspector="openInspector"
      />
      <PersonalDetailsPanel
        v-if="inspector.kind === 'personal'"
        :actions="actions"
        :personal="document.personalDetails"
      />
      <SectionPanel
        v-else-if="
          inspector.kind === 'section' && selectedSection !== undefined
        "
        :actions="actions"
        :section="selectedSection"
        :section-key="inspector.key"
      />
      <StructurePanel
        v-else-if="inspector.kind === 'structure'"
        :actions="actions"
      />
      <CustomizationPanel
        v-else-if="inspector.kind === 'customization'"
        :actions="actions"
        :record="record"
      />
      <TemplatePanel
        v-else-if="inspector.kind === 'templates'"
        :actions="actions"
        :record="record"
      />
      <PhotoPanel
        v-else-if="inspector.kind === 'photo'"
        :actions="actions"
        :record="record"
      />
    </aside>
  </main>
</template>
