<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core';
import { computed } from 'vue';

import AppSheetContent from '@/components/app/AppSheetContent.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { Sheet, SheetTitle } from '@/components/ui/sheet';
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs';
import {
  githubEvidenceSettingsCopy,
} from '@/i18n/github-evidence-settings';
import type { Locale } from '@/i18n/locale';
import { projectEvidenceCopy } from '@/i18n/project-evidence';
import { projectsCopy } from '@/i18n/projects';
import { shellCopy } from '@/i18n/shell';
import EvidenceCandidates from './EvidenceCandidates.vue';
import EvidenceLinkForm from './EvidenceLinkForm.vue';
import {
  PICKER_TABS,
  type EvidencePicker,
  type PickerTab,
} from './evidencePickerState';
import { evidenceNoticeText, startFailureText } from './evidenceText';
import type { EvidenceKind } from './projectDocument';

const props = defineProps<{
  readonly picker: EvidencePicker;
  readonly locale: Locale;
}>();
const emit = defineEmits<{ closed: [] }>();

const wide = useMediaQuery('(width >= 640px)');
const copy = computed(() => projectEvidenceCopy[props.locale]);
const projects = computed(() => projectsCopy[props.locale]);
const settings = computed(() => githubEvidenceSettingsCopy[props.locale]);
const settingsLink = computed(() => shellCopy[props.locale].settings);
const root = computed(() => (wide.value ? Dialog : Sheet));
const content = computed(() => (wide.value ? DialogContent : AppSheetContent));
const title = computed(() => (wide.value ? DialogTitle : SheetTitle));
const contentProps = computed(() => wide.value
  ? {
      class: 'max-h-[calc(100dvh-2rem)] gap-4 overflow-y-auto sm:max-w-xl',
      closeLabel: projects.value.close,
    }
  : {
      side: 'bottom' as const,
      class: 'max-h-[85dvh] gap-4 overflow-y-auto px-4 pt-5 '
        + 'pb-[calc(16px+env(safe-area-inset-bottom))]',
      closeLabel: projects.value.close,
    });
const notice = computed(() => {
  const current = props.picker.notice.value;
  return current === null ? '' : evidenceNoticeText(current, props.locale);
});
const startError = computed(() => {
  const failure = props.picker.startFailure.value;
  return failure === null ? '' : startFailureText(failure, props.locale);
});

function kindOf(tab: PickerTab): EvidenceKind {
  return tab === 'link' ? 'repository' : tab;
}

function onOpenChange(open: boolean): void {
  if (!open) props.picker.close();
}

function onOpenAutoFocus(event: Event): void {
  const container = event.target instanceof HTMLElement ? event.target : null;
  const active = container?.querySelector<HTMLElement>(
    '[role="tab"][data-state="active"]',
  );
  if (active === null || active === undefined) return;
  event.preventDefault();
  active.focus();
}

function onCloseAutoFocus(event: Event): void {
  event.preventDefault();
  emit('closed');
}
</script>

<template>
  <component
    :is="root"
    :open="picker.open.value"
    @update:open="onOpenChange"
  >
    <component
      :is="content"
      v-bind="contentProps"
      :aria-describedby="undefined"
      data-testid="evidence-dialog"
      @close-auto-focus="onCloseAutoFocus"
      @open-auto-focus="onOpenAutoFocus"
    >
      <component
        :is="title"
        class="pr-10 text-lg leading-snug font-bold"
      >
        {{ copy.add }}
      </component>
      <StatusBanner
        v-if="picker.outcome.value === 'connected'"
        kind="success"
        testid="evidence-outcome"
      >
        {{ settings.connectedNotice }}
      </StatusBanner>
      <StatusBanner
        v-else-if="picker.outcome.value === 'failed'"
        kind="error"
        testid="evidence-outcome"
      >
        {{ settings.failedNotice }}
      </StatusBanner>
      <StatusBanner
        v-else-if="picker.outcome.value === 'different-account'"
        kind="error"
        testid="evidence-outcome"
      >
        {{ settings.differentAccountNotice }}
        <NuxtLink
          class="text-link underline underline-offset-4"
          data-action="evidence-settings-link"
          to="/app/settings/sessions"
        >
          {{ settingsLink }}
        </NuxtLink>
      </StatusBanner>
      <p
        v-if="picker.firstAttach.value"
        class="text-sm text-muted-foreground"
        data-testid="evidence-first-attach"
      >
        {{ copy.firstAttach }}
      </p>
      <p
        v-if="picker.atLimit.value"
        class="text-sm"
        data-testid="evidence-limit"
      >
        {{ copy.limit }}
      </p>
      <p
        v-if="picker.locked.value"
        id="evidence-save-first"
        class="text-sm"
        data-testid="evidence-save-first"
      >
        {{ projects.saveFirst }}
      </p>
      <StatusBanner
        v-if="notice !== ''"
        kind="error"
        testid="evidence-dialog-notice"
      >
        {{ notice }}
      </StatusBanner>
      <Tabs
        class="min-w-0 gap-3"
        :model-value="picker.tab.value"
        @update:model-value="picker.select($event as PickerTab)"
      >
        <TabsList
          :aria-label="copy.add"
          class="h-auto max-w-full flex-wrap justify-start"
        >
          <TabsTrigger
            v-for="tab in PICKER_TABS"
            :key="tab"
            class="flex-none"
            :data-evidence-tab="tab"
            :value="tab"
          >
            {{ copy.tabs[tab] }}
          </TabsTrigger>
        </TabsList>
        <TabsContent
          v-for="tab in PICKER_TABS"
          :key="tab"
          class="grid min-w-0 gap-3"
          :value="tab"
        >
          <div
            v-if="picker.sessionEnded.value"
            class="grid gap-3"
            data-testid="evidence-session-ended"
          >
            <p class="text-sm">
              {{ copy.sessionEnded }}
            </p>
            <div>
              <Button
                :aria-describedby="picker.locked.value
                  ? 'evidence-save-first'
                  : undefined"
                data-action="evidence-continue"
                :disabled="picker.locked.value || picker.starting.value"
                type="button"
                @click="picker.continueWithGitHub()"
              >
                {{ settings.continue }}
              </Button>
            </div>
            <p
              v-if="startError !== ''"
              class="text-sm text-destructive"
              data-testid="evidence-start-error"
              role="alert"
            >
              {{ startError }}
            </p>
          </div>
          <EvidenceLinkForm
            v-else-if="tab === 'link'"
            :locale="locale"
            :picker="picker"
          />
          <template v-else>
            <p
              v-if="tab === 'release' && picker.repository.value === null"
              class="text-sm font-semibold"
              data-testid="evidence-choose-title"
            >
              {{ copy.chooseRepository }}
            </p>
            <div
              v-else-if="tab === 'release'"
              class="flex flex-wrap items-center justify-between gap-2"
            >
              <p
                class="min-w-0 font-semibold break-words"
                data-testid="evidence-chosen-repository"
              >
                {{ picker.repository.value?.fullName }}
              </p>
              <Button
                data-action="evidence-change-repository"
                size="sm"
                type="button"
                variant="outline"
                @click="picker.changeRepository()"
              >
                {{ copy.changeRepository }}
              </Button>
            </div>
            <EvidenceCandidates
              :kind="kindOf(tab)"
              :locale="locale"
              :picker="picker"
            />
          </template>
        </TabsContent>
      </Tabs>
    </component>
  </component>
</template>
