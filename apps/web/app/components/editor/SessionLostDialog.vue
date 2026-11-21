<script setup lang="ts">
import { computed, ref } from 'vue';

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button, buttonVariants } from '@/components/ui/button';
import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import { editorShellCopy } from '../../i18n/editor-shell';
import LocaleToggle from '../app/LocaleToggle.vue';

const props = defineProps<{
  readonly open: boolean;
  readonly actions: ResumeEditorActions;
}>();
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);
const firstAction = ref<{ $el?: HTMLElement } | null>(null);

function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  firstAction.value?.$el?.focus();
}

async function discardAndSignIn(): Promise<void> {
  props.actions.discard();
  await navigateTo('/login');
}
</script>

<template>
  <AlertDialog :open="open">
    <AlertDialogContent
      @escape-key-down.prevent
      @open-auto-focus="onOpenAutoFocus"
    >
      <AlertDialogHeader>
        <AlertDialogTitle>{{ copy.sessionLostTitle }}</AlertDialogTitle>
        <AlertDialogDescription>
          {{ copy.sessionLostDescription }}
        </AlertDialogDescription>
        <LocaleToggle
          :label="copy.localeLabel"
          @pointerdown.prevent
        />
      </AlertDialogHeader>
      <AlertDialogFooter>
        <Button
          ref="firstAction"
          variant="ghost"
          @click="discardAndSignIn"
        >
          {{ copy.discardAndSignIn }}
        </Button>
        <a
          :class="buttonVariants({ variant: 'outline' })"
          href="/login"
          target="_blank"
          rel="noopener noreferrer"
        >
          {{ copy.openSignIn }}
        </a>
        <Button
          data-action="resume-after-auth"
          @click="actions.resumeAfterAuth()"
        >
          {{ copy.resumeAfterSignIn }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
