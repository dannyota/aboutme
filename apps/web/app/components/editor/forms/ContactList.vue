<script setup lang="ts">
import type { PersonalDetail } from '@aboutme/schema';
import { computed, nextTick, ref, watch } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import { Ellipsis } from '@lucide/vue';
import IconButton from '../../app/IconButton.vue';
import SelectField from '../../app/SelectField.vue';
import StatusBanner from '../../app/StatusBanner.vue';
import TextField from '../../app/TextField.vue';
import { Button } from '../../ui/button';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../ui/dropdown-menu';

const props = defineProps<{
  readonly createEntityId: () => string;
  readonly details?: readonly PersonalDetail[];
}>();

const emit = defineEmits<{
  change: [details: readonly PersonalDetail[]];
  unset: [];
}>();

const details = ref<PersonalDetail[]>(copyDetails(props.details));
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].personal);
const root = ref<HTMLElement | null>(null);
const visibleLabels = ref<Record<string, boolean>>({});
const openMenuId = ref<string | null>(null);
const limitError = ref(false);
const urlError = ref<string | null>(null);

watch(
  () => props.details,
  (next) => {
    details.value = copyDetails(next);
    // A label revealed but not yet committed stays open while another
    // detail's change comes back; only removed details drop their flag.
    visibleLabels.value = Object.fromEntries(
      details.value
        .filter(
          (detail) =>
            detail.label !== undefined
            || visibleLabels.value[detail.id] === true,
        )
        .map((detail) => [detail.id, true]),
    );
    limitError.value = false;
  },
);

function showLabel(id: string): void {
  visibleLabels.value = { ...visibleLabels.value, [id]: true };
}

function labelVisible(detail: PersonalDetail): boolean {
  return detail.label !== undefined || visibleLabels.value[detail.id] === true;
}

function revealLabel(index: number): void {
  const detail = details.value[index];
  if (detail !== undefined) showLabel(detail.id);
}

async function focusField(
  index: number,
  field: 'value' | 'label' | 'type' | 'display' | 'is-hidden',
): Promise<void> {
  const detail = details.value[index];
  if (detail === undefined) return;
  if (field === 'label') showLabel(detail.id);
  if (field === 'is-hidden') openMenuId.value = detail.id;
  await nextTick();
  if (field === 'is-hidden') {
    const menu = Array.from(
      document.querySelectorAll<HTMLElement>('[data-detail-menu]'),
    ).find((candidate) => candidate.dataset.detailMenu === detail.id);
    menu?.querySelector<HTMLElement>('[data-detail-hide]')?.focus();
    return;
  }
  const row = Array.from(
    root.value?.querySelectorAll<HTMLElement>('[data-detail-index]') ?? [],
  ).find((candidate) => candidate.dataset.detailIndex === String(index));
  row?.querySelector<HTMLElement>(`[data-detail-${field}]`)?.focus();
}

defineExpose({ focusField, revealLabel });

function add(): void {
  if (details.value.length >= 16) {
    limitError.value = true;
    return;
  }
  limitError.value = false;
  replace([
    ...details.value,
    {
      id: props.createEntityId(),
      type: 'email',
      value: '',
      isHidden: false,
    },
  ]);
}

function changeLabel(id: string, value: string): void {
  const detail = detailById(id);
  if (detail === undefined || value === (detail.label ?? '')) return;
  replace(
    details.value.map((candidate) =>
      candidate.id === id
        ? value === '' && candidate.label === undefined
          ? candidate
          : { ...candidate, label: value }
        : candidate,
    ),
  );
}

function changeType(id: string, type: PersonalDetail['type']): void {
  const detail = detailById(id);
  if (detail === undefined || type === detail.type) return;
  if (
    isWebProfile(type)
    && detail.value !== ''
    && !detail.value.startsWith('https://')
  ) {
    urlError.value = id;
    return;
  }
  urlError.value = null;
  replace(
    details.value.map((candidate) =>
      candidate.id === id ? { ...candidate, type } : candidate,
    ),
  );
}

function changeValue(id: string, value: string): void {
  const detail = detailById(id);
  if (detail === undefined || value === detail.value) return;
  if (
    isWebProfile(detail.type)
    && value !== ''
    && !value.startsWith('https://')
  ) {
    urlError.value = id;
    return;
  }
  urlError.value = null;
  replace(
    details.value.map((candidate) =>
      candidate.id === id ? { ...candidate, value } : candidate,
    ),
  );
}

function changeHidden(id: string, isHidden: boolean): void {
  const detail = detailById(id);
  if (detail === undefined || isHidden === detail.isHidden) return;
  replace(
    details.value.map((candidate) =>
      candidate.id === id ? { ...candidate, isHidden } : candidate,
    ),
  );
}

// Absent `display` means short (ADR 0041), so choosing short drops the key.
function changeDisplay(id: string, display: LinkDisplay): void {
  const detail = detailById(id);
  if (detail === undefined || display === (detail.display ?? 'short')) return;
  replace(
    details.value.map((candidate) => {
      if (candidate.id !== id) return candidate;
      const { display: _display, ...withoutDisplay } = candidate;
      return display === 'short'
        ? withoutDisplay
        : { ...withoutDisplay, display };
    }),
  );
}

function unsetLabel(id: string): void {
  const detail = detailById(id);
  if (detail?.label === undefined) return;
  const { [id]: _cleared, ...stillVisible } = visibleLabels.value;
  visibleLabels.value = stillVisible;
  replace(
    details.value.map((candidate) => {
      if (candidate.id !== id) return candidate;
      const { label: _label, ...withoutLabel } = candidate;
      return withoutLabel;
    }),
  );
}

function move(id: string, direction: -1 | 1): void {
  const from = details.value.findIndex((detail) => detail.id === id);
  const to = from + direction;
  if (from < 0 || to < 0 || to >= details.value.length) return;
  const next = [...details.value];
  const [moved] = next.splice(from, 1);
  if (moved === undefined) return;
  next.splice(to, 0, moved);
  replace(next);
}

function remove(id: string): void {
  replace(details.value.filter((detail) => detail.id !== id));
}

function replace(next: readonly PersonalDetail[]): void {
  details.value = copyDetails(next);
  emit('change', details.value);
}

function detailById(id: string): PersonalDetail | undefined {
  return details.value.find((detail) => detail.id === id);
}

function copyDetails(
  value: readonly PersonalDetail[] | undefined,
): PersonalDetail[] {
  return value?.map((detail) => ({ ...detail })) ?? [];
}

function isWebProfile(type: PersonalDetail['type']): boolean {
  return (
    type === 'website'
    || type === 'linkedin'
    || type === 'github'
    || type === 'twitter'
  );
}

// Only a detail that renders as a link has a display choice: the four URL
// types, and a custom value with the renderer's exact https:// prefix.
function rendersAsLink(detail: PersonalDetail): boolean {
  return (
    isWebProfile(detail.type)
    || (detail.type === 'custom' && detail.value.startsWith('https://'))
  );
}

type LinkDisplay = NonNullable<PersonalDetail['display']>;

const displayOptions = computed(
  () =>
    [
      { value: 'short', label: copy.value.shortAddress },
      { value: 'full', label: copy.value.fullAddress },
      { value: 'label', label: copy.value.labelDisplay },
    ] as const,
);
const typeOptions = computed(
  () =>
    [
      { value: 'email', label: copy.value.email },
      { value: 'phone', label: copy.value.phone },
      { value: 'location', label: copy.value.location },
      { value: 'website', label: copy.value.website },
      { value: 'linkedin', label: copy.value.linkedin },
      { value: 'github', label: copy.value.github },
      { value: 'twitter', label: copy.value.twitter },
      { value: 'custom', label: copy.value.custom },
    ] as const,
);
</script>

<template>
  <div
    ref="root"
    class="grid gap-4"
  >
    <div
      v-for="(detail, index) in details"
      :key="detail.id"
      :data-detail-index="index"
      class="grid gap-2"
    >
      <h3
        class="sr-only"
        :data-detail-id="detail.id"
      >
        {{ copy.contactDetail }} {{ index + 1 }}
      </h3>
      <div class="grid gap-4">
        <!-- prettier-ignore -->
        <div
          class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]
            items-end gap-2"
        >
          <SelectField
            :label="copy.type"
            :model-value="detail.type"
            :options="typeOptions"
            :control-attrs="{ 'data-detail-type': '' }"
            @update:model-value="
              changeType(detail.id, $event as PersonalDetail['type'])
            "
          />
          <TextField
            :label="copy.value"
            :model-value="detail.value"
            :error="urlError === detail.id ? copy.urlError : undefined"
            :error-attrs="{ 'data-error': 'contact-url' }"
            :control-attrs="{ 'data-detail-value': '' }"
            @intent="
              (intent) =>
                intent.kind === 'unset'
                  ? changeValue(detail.id, '')
                  : changeValue(detail.id, intent.value)
            "
          />
          <DropdownMenu
            :open="openMenuId === detail.id"
            @update:open="(open) => (openMenuId = open ? detail.id : null)"
          >
            <DropdownMenuTrigger as-child>
              <IconButton
                :label="`${copy.moreOptions} ${index + 1}`"
                size="icon-sm"
                data-action="contact-detail-menu"
              >
                <Ellipsis />
              </IconButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="end"
              :data-detail-menu="detail.id"
            >
              <DropdownMenuItem
                data-action="set-detail-label"
                :aria-label="copy.setLabel"
                @select="showLabel(detail.id)"
              >
                {{ copy.setLabel }}
              </DropdownMenuItem>
              <DropdownMenuCheckboxItem
                data-action="toggle-detail-hidden"
                :model-value="detail.isHidden"
                :data-detail-hide="true"
                :aria-label="copy.hideDetail"
                @update:model-value="changeHidden(detail.id, $event)"
              >
                {{ copy.hideDetail }}
              </DropdownMenuCheckboxItem>
              <DropdownMenuItem
                data-action="move-detail-up"
                :disabled="index === 0"
                :aria-label="copy.moveUp"
                @select="move(detail.id, -1)"
              >
                {{ copy.moveUp }}
              </DropdownMenuItem>
              <DropdownMenuItem
                data-action="move-detail-down"
                :disabled="index === details.length - 1"
                :aria-label="copy.moveDown"
                @select="move(detail.id, 1)"
              >
                {{ copy.moveDown }}
              </DropdownMenuItem>
              <DropdownMenuItem
                data-action="remove-detail"
                :aria-label="copy.removeDetail"
                @select="remove(detail.id)"
              >
                {{ copy.removeDetail }}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <SelectField
          v-if="rendersAsLink(detail)"
          :label="copy.showAs"
          :hint="copy.labelHint"
          :model-value="detail.display ?? 'short'"
          :options="displayOptions"
          :control-attrs="{ 'data-detail-display': '' }"
          @update:model-value="changeDisplay(detail.id, $event as LinkDisplay)"
        />
        <TextField
          v-if="labelVisible(detail)"
          :label="copy.label"
          :model-value="detail.label"
          :control-attrs="{ 'data-detail-label': '' }"
          @intent="
            (intent) =>
              intent.kind === 'unset'
                ? unsetLabel(detail.id)
                : changeLabel(detail.id, intent.value)
          "
        />
      </div>
    </div>
    <div class="flex gap-2">
      <Button
        data-action="add-detail"
        size="sm"
        variant="outline"
        @click="add"
      >
        {{ copy.addDetail }}
      </Button>
      <Button
        v-if="props.details !== undefined"
        data-action="unset-details"
        size="sm"
        variant="ghost"
        @click="emit('unset')"
      >
        {{ copy.removeContactList }}
      </Button>
    </div>
    <StatusBanner
      v-if="limitError"
      kind="error"
      data-error="detail-limit"
    >
      {{ copy.contactLimit }}
    </StatusBanner>
  </div>
</template>
