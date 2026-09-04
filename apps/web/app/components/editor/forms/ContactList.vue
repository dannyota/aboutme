<script setup lang="ts">
import type { PersonalDetail } from '@aboutme/schema';
import { nextTick, ref, watch } from 'vue';
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
const root = ref<HTMLElement | null>(null);
const visibleLabels = ref<Record<string, boolean>>({});
const openMenuId = ref<string | null>(null);
const limitError = ref(false);
const urlError = ref<string | null>(null);

watch(
  () => props.details,
  (next) => {
    details.value = copyDetails(next);
    visibleLabels.value = Object.fromEntries(
      details.value
        .filter((detail) => detail.label !== undefined)
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
  field: 'value' | 'label' | 'type' | 'is-hidden',
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
  if (isWebProfile(type)
    && detail.value !== ''
    && !detail.value.startsWith('https://')) {
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

function unsetLabel(id: string): void {
  const detail = detailById(id);
  if (detail?.label === undefined) return;
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

const typeOptions = [
  { value: 'email', label: 'Email' },
  { value: 'phone', label: 'Phone' },
  { value: 'location', label: 'Location' },
  { value: 'website', label: 'Website' },
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'github', label: 'GitHub' },
  { value: 'twitter', label: 'Twitter' },
  { value: 'custom', label: 'Custom' },
] as const;
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
        Contact detail {{ index + 1 }}
      </h3>
      <div class="grid gap-4">
        <div
          class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]
            items-end gap-2"
        >
          <SelectField
            label="Type"
            :model-value="detail.type"
            :options="typeOptions"
            :control-attrs="{ 'data-detail-type': '' }"
            @update:model-value="
              changeType(detail.id, $event as PersonalDetail['type'])
            "
          />
          <TextField
            label="Value"
            :model-value="detail.value"
            :error="urlError === detail.id
              ? 'Use a lowercase https:// URL.' : undefined"
            :error-attrs="{ 'data-error': 'contact-url' }"
            :control-attrs="{ 'data-detail-value': '' }"
            @intent="(intent) => intent.kind === 'unset'
              ? changeValue(detail.id, '')
              : changeValue(detail.id, intent.value)"
          />
          <DropdownMenu
            :open="openMenuId === detail.id"
            @update:open="(open) => openMenuId = open ? detail.id : null"
          >
            <DropdownMenuTrigger as-child>
              <IconButton
                :label="`More options for contact detail ${index + 1}`"
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
                aria-label="Set label…"
                @select="showLabel(detail.id)"
              >
                Set label…
              </DropdownMenuItem>
              <DropdownMenuCheckboxItem
                data-action="toggle-detail-hidden"
                :model-value="detail.isHidden"
                :data-detail-hide="true"
                aria-label="Hide this detail"
                @update:model-value="changeHidden(detail.id, $event)"
              >
                Hide this detail
              </DropdownMenuCheckboxItem>
              <DropdownMenuItem
                data-action="move-detail-up"
                :disabled="index === 0"
                aria-label="Move up"
                @select="move(detail.id, -1)"
              >
                Move up
              </DropdownMenuItem>
              <DropdownMenuItem
                data-action="move-detail-down"
                :disabled="index === details.length - 1"
                aria-label="Move down"
                @select="move(detail.id, 1)"
              >
                Move down
              </DropdownMenuItem>
              <DropdownMenuItem
                data-action="remove-detail"
                aria-label="Remove detail"
                @select="remove(detail.id)"
              >
                Remove detail
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <TextField
          v-if="labelVisible(detail)"
          label="Label"
          :model-value="detail.label"
          :control-attrs="{ 'data-detail-label': '' }"
          @intent="(intent) => intent.kind === 'unset'
            ? unsetLabel(detail.id) : changeLabel(detail.id, intent.value)"
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
        Add detail
      </Button>
      <Button
        v-if="props.details !== undefined"
        data-action="unset-details"
        size="sm"
        variant="ghost"
        @click="emit('unset')"
      >
        Remove contact list
      </Button>
    </div>
    <StatusBanner
      v-if="limitError"
      kind="error"
      data-error="detail-limit"
    >
      You can add up to 16 contact details.
    </StatusBanner>
  </div>
</template>
