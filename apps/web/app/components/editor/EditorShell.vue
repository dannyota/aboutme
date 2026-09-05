<script setup lang="ts">
import {
  ChevronDown,
  FileText,
  Image,
  LayoutList,
  Palette,
  PanelsTopLeft,
  Sparkles,
} from '@lucide/vue';
import { computed, nextTick, ref } from 'vue';
import { Button, buttonVariants } from '@/components/ui/button';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible';
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import IconButton from '../app/IconButton.vue';
import StateMark from '../app/StateMark.vue';
import { iconFor } from '../resume/icons';

import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import { useStamp } from '../../composables/useStamp';
import { canonicalPublicPath } from '../../editor/publishApi';
import type { SaveState } from '../../editor/types';
import type { ResumeRecord } from '../../stores/resumes';
import AccountMenu from '../app/AccountMenu.vue';
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
import PDFDownloadButton from './PDFDownloadButton.vue';

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
const outlineOpen = ref(true);
const publishOpen = ref(false);
const zoom = ref<'fit' | 'full'>('fit');
const document = computed(() => props.record.current.document);
const publicLink = computed(() =>
  props.record.accepted.metadata.live
    ? canonicalPublicPath(props.record.accepted.metadata.slug)
    : null,
);
const { displayLink, stampState } = useStamp(publicLink);
const placement = computed(() => document.value.customization.layout.sections);
const outline = computed(() => [
  {
    key: 'personal',
    label: 'Personal details',
    iconKey: 'user',
    icon: iconFor('user') ?? PanelsTopLeft,
  },
  ...[...placement.value.main, ...placement.value.sidebar].flatMap((key) => {
    const section = document.value.content[key];
    return section === undefined
      ? []
      : [
          {
            key,
            label: section.displayName ?? sectionLabel(section.sectionType),
            iconKey: section.iconKey ?? '',
            icon: iconFor(section.iconKey ?? '') ?? PanelsTopLeft,
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
  <main
    :class="[
      'editor-shell grid min-h-dvh max-h-dvh',
      'grid-cols-[4rem_16.5rem_minmax(32rem,1fr)_22rem]',
      'grid-rows-[4rem_minmax(0,1fr)] overflow-hidden bg-background',
      'text-foreground max-[72rem]:grid-cols-[16.5rem_minmax(0,1fr)]',
      'max-[42rem]:grid-cols-[minmax(0,1fr)]',
    ]"
  >
    <header
      class="editor-topbar col-span-full flex h-16 items-center gap-4
        border-b bg-background/95 px-4"
      data-region="topbar"
    >
      <NuxtLink
        class="editor-brand font-semibold"
        to="/app/resumes"
      > aboutme </NuxtLink>
      <span
        aria-hidden="true"
        class="h-5 border-l max-[42rem]:hidden"
      />
      <h1
        class="min-w-0 truncate text-sm font-medium max-[42rem]:hidden"
        data-resume-title
      >
        {{ record.current.metadata.title }}
      </h1>
      <SaveStatus
        class="shrink-0"
        data-testid="save-status"
        :state="saveState"
      />
      <StateMark
        v-if="displayLink !== null"
        class="shrink-0 max-[42rem]:hidden"
        :data-stamp="stampState === 'idle' ? undefined : stampState"
        data-testid="public-mark"
        :link="displayLink"
        state="public"
      />
      <span class="flex-1" />
      <PDFDownloadButton :controller="actions.downloadPdf" />
      <Button
        class="editor-publish-action"
        data-action="publish"
        size="sm"
        type="button"
        variant="seal"
        @click="publishOpen = true"
      >
        Publish
      </Button>
      <div
        class="editor-account-actions flex items-center"
        data-region="account-actions"
      >
        <AccountMenu />
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
        gap-2 border-r bg-card p-2 max-[72rem]:col-start-2
        max-[72rem]:z-20 max-[72rem]:h-14 max-[72rem]:flex-row
        max-[72rem]:border-b max-[72rem]:border-r-0 max-[42rem]:col-start-1
        max-[72rem]:data-[narrow-active=false]:pointer-events-none
        max-[72rem]:data-[narrow-active=false]:invisible
        max-[72rem]:data-[narrow-active=false]:opacity-0"
      aria-label="Editor tools"
      :data-narrow-active="narrowRegion === 'editor'"
      data-region="app-rail"
      role="toolbar"
    >
      <IconButton
        class="aria-pressed:bg-secondary aria-pressed:text-primary"
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
        class="aria-pressed:bg-secondary aria-pressed:text-primary"
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
        class="aria-pressed:bg-secondary aria-pressed:text-primary"
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
        class="aria-pressed:bg-secondary aria-pressed:text-primary"
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
        class="aria-pressed:bg-secondary aria-pressed:text-primary"
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
    </aside>

    <aside
      class="editor-outline col-start-2 row-start-2 flex min-w-0 flex-col
        overflow-auto border-r bg-card max-[72rem]:col-start-1
        max-[72rem]:row-start-2 max-[72rem]:w-full max-[72rem]:max-w-[38rem]
        max-[72rem]:data-[narrow-active=false]:pointer-events-none
        max-[72rem]:data-[narrow-active=false]:invisible max-[42rem]:hidden"
      data-region="outline"
      data-responsive-region="editor"
      :data-narrow-active="narrowRegion === 'editor'"
    >
      <Collapsible
        v-model:open="outlineOpen"
        class="flex min-h-0 flex-1 flex-col"
      >
        <div class="flex items-center justify-between p-4">
          <h2 class="text-base font-semibold">
            Resume
          </h2>
          <div class="flex items-center gap-1">
            <CollapsibleTrigger as-child>
              <IconButton
                label="Toggle resume outline"
                size="icon-sm"
              >
                <ChevronDown
                  aria-hidden="true"
                  :class="outlineOpen ? '' : '-rotate-90'"
                />
              </IconButton>
            </CollapsibleTrigger>
            <IconButton
              label="Add section"
              size="icon-sm"
              @click="inspector = { kind: 'structure' }"
            >
              +
            </IconButton>
          </div>
        </div>
        <CollapsibleContent class="flex min-h-0 flex-1 flex-col">
          <nav
            aria-label="Resume outline"
            class="grid gap-1 px-2"
          >
            <Button
              v-for="item in outline"
              :key="item.key"
              :aria-current="
                (inspector.kind === 'section' && inspector.key === item.key)
                  || (inspector.kind === 'personal'
                    && item.key === 'personal')
                  ? 'page'
                  : undefined
              "
              :data-outline-key="item.key"
              class="w-full justify-start aria-[current=page]:bg-accent
                aria-[current=page]:text-accent-foreground"
              variant="ghost"
              @click="selectOutline(item.key)"
            >
              <component
                :is="item.icon"
                :size="18"
                aria-hidden="true"
                :data-icon-key="item.iconKey"
                data-outline-icon
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
        </CollapsibleContent>
      </Collapsible>
    </aside>

    <div
      class="editor-preview-region relative col-start-3 row-start-2 grid
        min-w-0 grid-rows-[minmax(0,1fr)] overflow-hidden bg-background
        max-[72rem]:col-span-2 max-[72rem]:col-start-1 max-[72rem]:row-start-2
        max-[72rem]:data-[narrow-active=false]:pointer-events-none
        max-[72rem]:data-[narrow-active=false]:invisible"
      data-region="preview"
      data-responsive-region="preview"
      :data-narrow-active="narrowRegion === 'preview'"
    >
      <EditorPreview
        :active="narrowRegion === 'preview'"
        :document="document"
        :lng="record.current.metadata.lng"
        :photo-read="record.photoRead"
        :photo-url="photoUrl"
        :public-link="displayLink"
        :stamp-state="stampState"
        :zoom="zoom"
      />
    </div>

    <aside
      class="editor-inspector col-start-4 row-start-2 min-w-0 overflow-auto
        border-l bg-card p-4 max-[72rem]:col-start-2 max-[72rem]:row-start-2
        max-[72rem]:ml-0 max-[72rem]:w-full max-[72rem]:max-w-none
        max-[72rem]:pt-20 max-[42rem]:col-start-1 max-[42rem]:w-full
        max-[42rem]:max-w-none max-[42rem]:pb-24
        max-[72rem]:data-[narrow-active=false]:pointer-events-none
        max-[72rem]:data-[narrow-active=false]:invisible"
      data-region="inspector"
      data-responsive-region="editor"
      :data-narrow-active="narrowRegion === 'editor'"
    >
      <Sheet>
        <SheetTrigger as-child>
          <Button
            class="mb-4 w-full justify-between min-[42rem]:hidden"
            data-action="open-sections"
            type="button"
            variant="outline"
          >
            Sections
            <ChevronDown aria-hidden="true" />
          </Button>
        </SheetTrigger>
        <SheetContent
          class="w-[calc(100%-2rem)] max-w-sm"
          side="left"
        >
          <SheetHeader>
            <SheetTitle>Resume</SheetTitle>
          </SheetHeader>
          <nav
            aria-label="Resume sections"
            class="grid gap-1 px-2"
          >
            <SheetClose
              v-for="item in outline"
              :key="item.key"
              as-child
            >
              <Button
                :aria-current="
                  (inspector.kind === 'section' && inspector.key === item.key)
                    || (inspector.kind === 'personal'
                      && item.key === 'personal')
                    ? 'page'
                    : undefined
                "
                :data-outline-key="item.key"
                class="w-full justify-start aria-[current=page]:bg-accent
                  aria-[current=page]:text-accent-foreground"
                type="button"
                variant="ghost"
                @click="selectOutline(item.key)"
              >
                <component
                  :is="item.icon"
                  :size="18"
                  aria-hidden="true"
                  :data-icon-key="item.iconKey"
                  data-outline-icon
                />
                <span class="truncate">{{ item.label }}</span>
              </Button>
            </SheetClose>
          </nav>
          <SheetClose as-child>
            <Button
              class="mx-4 mt-auto mb-4"
              type="button"
              variant="outline"
              @click="inspector = { kind: 'structure' }"
            >
              + Add section
            </Button>
          </SheetClose>
        </SheetContent>
      </Sheet>
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

    <div
      aria-label="Editor view"
      class="fixed inset-x-0 bottom-0 z-40 grid grid-cols-2 border-t
        bg-background p-2 pb-[calc(0.5rem+env(safe-area-inset-bottom))]
        min-[42rem]:hidden"
      role="tablist"
    >
      <Button
        :aria-pressed="narrowRegion === 'editor'"
        :aria-selected="narrowRegion === 'editor'"
        data-action="show-editor"
        role="tab"
        type="button"
        :variant="narrowRegion === 'editor' ? 'secondary' : 'ghost'"
        @click="narrowRegion = 'editor'"
      >
        Edit
      </Button>
      <Button
        :aria-pressed="narrowRegion === 'preview'"
        :aria-selected="narrowRegion === 'preview'"
        data-action="show-preview"
        role="tab"
        type="button"
        :variant="narrowRegion === 'preview' ? 'secondary' : 'ghost'"
        @click="narrowRegion = 'preview'"
      >
        Preview
      </Button>
    </div>

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
