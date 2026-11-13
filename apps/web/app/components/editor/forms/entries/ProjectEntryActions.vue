<script setup lang="ts">
import { ChevronDown } from '@lucide/vue';
import { computed } from 'vue';

import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import type { EditorFieldsCopy } from '@/i18n/editor-fields';
import { projectName } from '../../../projects/projectDocument';
import type { ProjectLibraryView } from '../projectLibrary';

const props = defineProps<{
  readonly linked: boolean;
  readonly library: ProjectLibraryView;
  readonly shown: ReadonlySet<string>;
  readonly busy: boolean;
  readonly atLimit: boolean;
  readonly limitLine: string;
  readonly status: string;
  readonly savedProjectId: string | null;
  readonly copy: EditorFieldsCopy['projectLinks'];
}>();
const emit = defineEmits<{
  link: [projectId: string];
  save: [];
  retry: [];
}>();

const available = computed(() => props.library.kind === 'ready'
  ? props.library.items.filter((item) => !props.shown.has(item.id))
  : []);
const loading = computed(() =>
  props.library.kind === 'idle' || props.library.kind === 'loading');
</script>

<template>
  <div class="mt-3 grid gap-2">
    <div
      v-if="!linked"
      class="flex flex-wrap items-center gap-2"
      data-project-entry-actions
    >
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <Button
            data-action="link-entry-project"
            :disabled="busy || loading"
            size="sm"
            type="button"
            variant="ghost"
          >
            {{ copy.linkToProject }}
            <ChevronDown aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          class="max-w-[calc(100vw-2rem)]"
        >
          <DropdownMenuItem
            v-if="library.kind === 'failed'"
            data-action="retry-project-library"
            @select="emit('retry')"
          >
            {{ copy.retry }}
          </DropdownMenuItem>
          <template v-else>
            <DropdownMenuItem
              v-for="item in available"
              :key="item.id"
              :data-entry-project-option="item.id"
              @select="emit('link', item.id)"
            >
              <span class="truncate">{{ projectName(item) }}</span>
            </DropdownMenuItem>
            <DropdownMenuItem
              v-if="available.length === 0"
              as-child
            >
              <NuxtLink
                data-action="open-projects"
                to="/app/projects"
              >
                {{ copy.openProjects }}
              </NuxtLink>
            </DropdownMenuItem>
          </template>
        </DropdownMenuContent>
      </DropdownMenu>
      <p
        v-if="atLimit"
        class="text-sm text-muted-foreground"
        data-project-limit
      >
        {{ limitLine }}
      </p>
      <Button
        v-else
        data-action="save-entry-project"
        :disabled="busy"
        size="sm"
        type="button"
        variant="ghost"
        @click="emit('save')"
      >
        {{ copy.saveToProjects }}
      </Button>
    </div>
    <p
      class="text-sm text-muted-foreground empty:hidden"
      data-entry-project-status
      role="status"
    >
      <template v-if="status !== ''">
        {{ status }}
        <NuxtLink
          v-if="savedProjectId !== null"
          class="underline"
          data-action="open-saved-project"
          :to="`/app/projects/${encodeURIComponent(savedProjectId)}`"
        >
          {{ copy.openProject }}
        </NuxtLink>
      </template>
    </p>
  </div>
</template>
