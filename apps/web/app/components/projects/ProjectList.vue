<script setup lang="ts">
import { Ellipsis } from '@lucide/vue';

import IconButton from '@/components/app/IconButton.vue';
import { Badge } from '@/components/ui/badge';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import type { Locale } from '@/i18n/locale';
import type { ProjectsCopy } from '@/i18n/projects';
import { formatRelativeTime } from '@/utils/relativeTime';
import {
  projectName,
  projectRole,
  type Project,
  type ProjectTextDraft,
} from './projectDocument';

const props = defineProps<{
  readonly items: readonly Project[];
  readonly now: Date;
  readonly copy: ProjectsCopy;
  readonly locale: Locale;
}>();
const emit = defineEmits<{
  changeAddress: [project: Project];
  remove: [project: Project];
}>();

const SHOWN_TECHNOLOGIES = 3;

function nameOf(project: Project): string {
  return projectName(project).trim() || props.copy.untitled;
}

function shownTechnologies(project: Project): readonly string[] {
  return (project.technologies ?? []).slice(0, SHOWN_TECHNOLOGIES);
}

function hiddenTechnologies(project: Project): number {
  return Math.max(
    0, (project.technologies?.length ?? 0) - SHOWN_TECHNOLOGIES,
  );
}

function hasText(text: Partial<ProjectTextDraft> | undefined): boolean {
  return Object.values(text ?? {}).some((value) => value !== '');
}

function languages(project: Project): readonly string[] {
  return Object.keys(project.text).filter((key) =>
    key === project.defaultLanguage || hasText(project.text[key]));
}

function languageLabel(project: Project, key: string): string {
  return props.copy.languageChip(
    key.toUpperCase(), key === project.defaultLanguage,
  );
}
</script>

<template>
  <ul
    :aria-label="copy.listLabel"
    class="divide-y divide-border rounded-lg border border-border bg-card"
    data-testid="project-list"
  >
    <li
      v-for="project in items"
      :key="project.id"
      :data-testid="`project-row-${project.id}`"
      class="relative grid min-w-0 gap-1.5 py-3 pr-14 pl-4
        sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-center sm:gap-x-6"
    >
      <div class="min-w-0">
        <NuxtLink
          :to="`/app/projects/${encodeURIComponent(project.id)}`"
          class="block truncate font-semibold -outline-offset-2
            after:absolute after:inset-0"
          data-project-link
        >
          {{ nameOf(project) }}
        </NuxtLink>
        <p
          v-if="projectRole(project) !== ''"
          class="truncate text-sm text-muted-foreground"
          data-project-role
        >
          {{ projectRole(project) }}
        </p>
      </div>
      <div class="flex min-w-0 flex-wrap gap-1.5">
        <ul
          v-if="shownTechnologies(project).length > 0"
          :aria-label="copy.technologiesLabel(nameOf(project))"
          class="flex min-w-0 flex-wrap gap-1.5"
          data-project-technologies
        >
          <li
            v-for="(technology, index) in shownTechnologies(project)"
            :key="`${index}-${technology}`"
            class="max-w-full"
          >
            <Badge
              class="max-w-full"
              variant="secondary"
            >
              <span class="truncate">{{ technology }}</span>
            </Badge>
          </li>
          <li v-if="hiddenTechnologies(project) > 0">
            <Badge variant="outline">
              {{ copy.moreTechnologies(hiddenTechnologies(project)) }}
            </Badge>
          </li>
        </ul>
        <ul
          :aria-label="copy.languagesLabel(nameOf(project))"
          class="flex flex-wrap gap-1.5"
          data-project-languages
        >
          <li
            v-for="key in languages(project)"
            :key="key"
          >
            <Badge
              :data-default="key === project.defaultLanguage || undefined"
              variant="outline"
            >
              {{ languageLabel(project, key) }}
            </Badge>
          </li>
        </ul>
      </div>
      <time
        class="text-xs text-muted-foreground tabular-nums"
        :datetime="project.updatedAt"
      >
        {{ copy.updated(formatRelativeTime(project.updatedAt, now, locale)) }}
      </time>
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <IconButton
            class="absolute top-2 right-2 z-10 sm:top-1/2 sm:-translate-y-1/2"
            :label="copy.moreActions(nameOf(project))"
            :data-project-actions="project.id"
            size="icon-sm"
          >
            <Ellipsis aria-hidden="true" />
          </IconButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            data-action="project-change-address"
            @select="emit('changeAddress', project)"
          >
            {{ copy.changeAddress }}
          </DropdownMenuItem>
          <DropdownMenuItem
            class="text-destructive"
            data-action="project-delete"
            variant="destructive"
            @select="emit('remove', project)"
          >
            {{ copy.delete }}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  </ul>
</template>
