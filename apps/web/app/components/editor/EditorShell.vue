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
import { Button, buttonVariants } from '@/components/ui/button';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import IconButton from '../app/IconButton.vue';

import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import type { SaveState } from '../../editor/types';
import type { ResumeRecord } from '../../stores/resumes';
import AccountMenu from '../app/AccountMenu.vue';
import ThemeToggle from '../app/ThemeToggle.vue';
import CustomizationPanel from './customization/CustomizationPanel.vue';
import PersonalDetailsPanel from './forms/PersonalDetailsPanel.vue';
import SectionPanel from './forms/SectionPanel.vue';
import PhotoPanel from './photo/PhotoPanel.vue';
import StructurePanel from './structure/StructurePanel.vue';
import TemplatePanel from './templates/TemplatePanel.vue';
import ConflictPanel from './ConflictPanel.vue';
import EditorPreview from './EditorPreview.vue';
import ErrorSummary from './ErrorSummary.vue';
import PreviewToolbar from './PreviewToolbar.vue';
import SaveStatus from './SaveStatus.vue';
import PublishDialog from './PublishDialog.vue';
import { photoStateFor } from './previewProjection';

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
const estimatedPages = ref<number | null>(null);
const zoom = ref<'fit' | 'full'>('fit');
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
const photoState = computed(() =>
  photoStateFor(
    props.record.photoRead,
    document.value.personalDetails.photo !== undefined,
  ),
);
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
  <main
    :class="[
      'editor-shell grid min-h-dvh max-h-dvh',
      'grid-cols-[4rem_16.5rem_minmax(32rem,1fr)_22rem]',
      'grid-rows-[4rem_minmax(0,1fr)] overflow-hidden bg-background',
      'text-foreground max-[72rem]:grid-cols-[4rem_minmax(0,1fr)]',
    ]"
  >
    <header
      :class="[
        'editor-topbar col-span-full grid',
        'grid-cols-[auto_minmax(12rem,1fr)_auto_auto_auto] items-center gap-5',
        'border-b bg-background/95 px-4 py-2.5',
        'max-[72rem]:grid-cols-[auto_minmax(0,1fr)_auto_auto_auto]',
        'max-[72rem]:grid-rows-[4rem]',
        'max-[42rem]:gap-3',
      ]"
    >
      <NuxtLink
        class="editor-brand"
        to="/app/resumes"
      > aboutme </NuxtLink>
      <div class="editor-title-group flex min-w-0 items-center gap-4">
        <h1
          class="truncate text-sm font-semibold"
          data-resume-title
        >
          {{ record.current.metadata.title }}
        </h1>
        <SaveStatus :state="saveState" />
      </div>
      <Button
        class="editor-publish-action"
        data-action="publish"
        size="sm"
        type="button"
        @click="publishOpen = true"
      >
        Publish
      </Button>
      <div
        class="editor-view-switcher flex items-center gap-1"
        aria-label="Editor view"
      >
        <Button
          :aria-pressed="narrowRegion === 'editor'"
          :data-action="'show-editor'"
          :variant="narrowRegion === 'editor' ? 'default' : 'outline'"
          size="sm"
          type="button"
          @click="narrowRegion = 'editor'"
        >
          Editor
        </Button>
        <Button
          :aria-pressed="narrowRegion === 'preview'"
          :data-action="'show-preview'"
          :variant="narrowRegion === 'preview' ? 'default' : 'outline'"
          size="sm"
          type="button"
          @click="narrowRegion = 'preview'"
        >
          Preview
        </Button>
      </div>
      <div class="editor-account-actions flex items-center gap-2">
        <AccountMenu />
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

    <aside
      class="editor-app-rail col-start-1 row-start-2 flex flex-col items-center
        gap-2 border-r bg-card p-2"
      data-region="app-rail"
      aria-label="Editor tools"
    >
      <IconButton
        data-action="open-document"
        :pressed="inspector.kind === 'personal' || inspector.kind === 'section'"
        label="Document"
        @click="inspector = { kind: 'personal' }"
      >
        <FileText
          :size="20"
          aria-hidden="true"
        />
      </IconButton>
      <IconButton
        data-action="open-structure"
        :pressed="inspector.kind === 'structure'"
        label="Structure"
        @click="inspector = { kind: 'structure' }"
      >
        <LayoutList
          :size="20"
          aria-hidden="true"
        />
      </IconButton>
      <IconButton
        data-action="open-design"
        :pressed="inspector.kind === 'customization'"
        label="Design"
        @click="inspector = { kind: 'customization' }"
      >
        <Palette
          :size="20"
          aria-hidden="true"
        />
      </IconButton>
      <IconButton
        data-action="open-templates"
        :pressed="inspector.kind === 'templates'"
        label="Templates"
        @click="inspector = { kind: 'templates' }"
      >
        <Sparkles
          :size="20"
          aria-hidden="true"
        />
      </IconButton>
      <IconButton
        data-action="open-photo"
        :pressed="inspector.kind === 'photo'"
        label="Photo"
        @click="inspector = { kind: 'photo' }"
      >
        <Image
          :size="20"
          aria-hidden="true"
        />
      </IconButton>
      <span class="flex-1" />
      <NuxtLink
        :class="buttonVariants({ variant: 'ghost', size: 'icon' })"
        to="/app/settings/sessions"
        aria-label="Account settings"
      >
        <Settings2
          :size="20"
          aria-hidden="true"
        />
      </NuxtLink>
      <span
        class="grid size-9 place-items-center rounded-full border"
        aria-hidden="true"
      >
        <UserRound :size="18" />
      </span>
    </aside>

    <aside
      class="editor-outline col-start-2 row-start-2 flex min-w-0 flex-col
        overflow-auto border-r bg-card max-[72rem]:col-start-2
        max-[72rem]:row-start-2 max-[72rem]:w-full max-[72rem]:max-w-[38rem]
        max-[72rem]:data-[narrow-active=false]:pointer-events-none
        max-[72rem]:data-[narrow-active=false]:invisible"
      data-region="outline"
      data-responsive-region="editor"
      :data-narrow-active="narrowRegion === 'editor'"
    >
      <div class="flex items-center justify-between p-4">
        <h2 class="text-base font-semibold">
          Resume
        </h2>
        <IconButton
          label="Add section"
          size="icon-sm"
          @click="inspector = { kind: 'structure' }"
        >
          +
        </IconButton>
      </div>
      <nav
        class="grid gap-1 px-2"
        aria-label="Resume outline"
      >
        <Button
          v-for="item in outline"
          :key="item.key"
          :aria-current="
            (inspector.kind === 'section' && inspector.key === item.key)
              || (inspector.kind === 'personal' && item.key === 'personal')
              ? 'page'
              : undefined
          "
          :data-outline-key="item.key"
          class="w-full justify-start aria-[current=page]:bg-positive/15
            aria-[current=page]:text-foreground"
          variant="ghost"
          @click="selectOutline(item.key)"
        >
          <PanelsTopLeft
            :size="18"
            aria-hidden="true"
          />
          <span class="truncate">{{ item.label }}</span>
        </Button>
      </nav>
      <Button
        class="mx-4 mt-auto mb-4"
        variant="outline"
        @click="inspector = { kind: 'structure' }"
      >
        + Add section
      </Button>
    </aside>

    <div
      class="editor-preview-region col-start-3 row-start-2 grid min-w-0
        grid-rows-[auto_minmax(0,1fr)] overflow-hidden bg-muted
        max-[72rem]:col-start-2 max-[72rem]:row-start-2
        max-[72rem]:data-[narrow-active=false]:pointer-events-none
        max-[72rem]:data-[narrow-active=false]:invisible"
      data-region="preview"
      data-responsive-region="preview"
      :data-narrow-active="narrowRegion === 'preview'"
    >
      <PreviewToolbar
        :estimated-pages="estimatedPages"
        :photo-state="photoState"
        :zoom="zoom"
        @open-photo="inspector = { kind: 'photo' }"
        @update:zoom="zoom = $event"
      />
      <EditorPreview
        :document="document"
        :lng="record.current.metadata.lng"
        :photo-read="record.photoRead"
        :photo-url="photoUrl"
        :zoom="zoom"
        @pages="estimatedPages = $event"
      />
    </div>

    <aside
      class="editor-inspector col-start-4 row-start-2 min-w-0 overflow-auto
        border-l bg-card p-4 max-[72rem]:col-start-2 max-[72rem]:row-start-2
        max-[72rem]:ml-[min(16.5rem,38%)] max-[72rem]:w-[min(100%,38rem)]
        max-[72rem]:data-[narrow-active=false]:pointer-events-none
        max-[72rem]:data-[narrow-active=false]:invisible"
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

    <AlertDialog :open="record.sessionLost">
      <AlertDialogContent @escape-key-down.prevent>
        <AlertDialogHeader>
          <AlertDialogTitle>Sign in to continue editing</AlertDialogTitle>
          <AlertDialogDescription>
            Your unsaved work is still open in this tab.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <a
            :class="buttonVariants({ variant: 'outline' })"
            href="/login"
            target="_blank"
            rel="noopener noreferrer"
          >
            Open sign-in in another tab
          </a>
          <Button
            data-action="resume-after-auth"
            @click="actions.resumeAfterAuth()"
          >
            Resume after sign-in
          </Button>
          <Button
            variant="ghost"
            @click="discardAndSignIn"
          >
            Discard and sign in
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </main>
</template>
