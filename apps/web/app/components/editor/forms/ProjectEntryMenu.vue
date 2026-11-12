<script setup lang="ts">
import { ChevronDown } from '@lucide/vue';
import { computed } from 'vue';

import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import type { EditorFieldsCopy } from '@/i18n/editor-fields';
import { projectName } from '../../projects/projectDocument';
import type { ProjectLibraryView } from './projectLibrary';

// A project section's Add entry menu (docs/design/ui/projects.md "In the
// resume editor"): a blank entry, or one of the library projects this resume
// does not show yet.
const props = defineProps<{
  readonly library: ProjectLibraryView;
  readonly linked: ReadonlySet<string>;
  readonly busy: boolean;
  readonly addLabel: string;
  readonly copy: EditorFieldsCopy['projectLinks'];
}>();
const emit = defineEmits<{
  blank: [];
  link: [projectId: string];
  retry: [];
}>();

const available = computed(() => props.library.kind === 'ready'
  ? props.library.items.filter((item) => !props.linked.has(item.id))
  : []);
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button
        data-action="add-entry"
        size="sm"
      >
        {{ addLabel }}
        <ChevronDown aria-hidden="true" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent
      align="end"
      class="max-w-[calc(100vw-2rem)]"
    >
      <DropdownMenuItem
        data-action="add-blank-entry"
        @select="emit('blank')"
      >
        {{ copy.blankEntry }}
      </DropdownMenuItem>
      <DropdownMenuItem
        v-if="library.kind === 'failed'"
        data-action="retry-project-library"
        @select="emit('retry')"
      >
        {{ copy.retry }}
      </DropdownMenuItem>
      <DropdownMenuSub v-else>
        <DropdownMenuSubTrigger
          data-action="from-projects"
          :disabled="library.kind !== 'ready' || busy"
        >
          {{ copy.fromProjects }}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent class="max-w-[calc(100vw-2rem)]">
          <DropdownMenuItem
            v-for="item in available"
            :key="item.id"
            :data-project-option="item.id"
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
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
