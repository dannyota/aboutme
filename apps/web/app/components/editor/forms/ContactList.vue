<script setup lang="ts">
import type { PersonalDetail } from '@aboutme/schema';
import { ref, watch } from 'vue';

const props = defineProps<{
  readonly createEntityId: () => string;
  readonly details?: readonly PersonalDetail[];
}>();

const emit = defineEmits<{
  change: [details: readonly PersonalDetail[]];
  unset: [];
}>();

const details = ref<PersonalDetail[]>(copyDetails(props.details));
const limitError = ref(false);
const urlError = ref<string | null>(null);

watch(
  () => props.details,
  (next) => {
    details.value = copyDetails(next);
    limitError.value = false;
  },
);

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
</script>

<template>
  <fieldset>
    <legend>Contact details</legend>
    <ol>
      <li
        v-for="(detail, index) in details"
        :key="detail.id"
        :data-detail-index="index"
      >
        <span data-detail-id>{{ detail.id }}</span>
        <label>
          Type
          <select
            data-detail-type
            :value="detail.type"
            @change="
              changeType(
                detail.id,
                ($event.target as HTMLSelectElement)
                  .value as PersonalDetail['type'],
              )
            "
          >
            <option value="email">Email</option>
            <option value="phone">Phone</option>
            <option value="location">Location</option>
            <option value="website">Website</option>
            <option value="linkedin">LinkedIn</option>
            <option value="github">GitHub</option>
            <option value="twitter">Twitter</option>
            <option value="custom">Custom</option>
          </select>
        </label>
        <label>
          Label
          <input
            data-detail-label
            :value="detail.label ?? ''"
            @blur="
              changeLabel(detail.id, ($event.target as HTMLInputElement).value)
            "
          >
        </label>
        <button
          type="button"
          data-action="unset-detail-label"
          @click="unsetLabel(detail.id)"
        >
          Remove label
        </button>
        <label>
          Value
          <input
            data-detail-value
            :value="detail.value"
            :aria-describedby="
              urlError === detail.id ? `contact-url-${index}` : undefined
            "
            @blur="
              changeValue(detail.id, ($event.target as HTMLInputElement).value)
            "
          >
        </label>
        <p
          v-if="urlError === detail.id"
          :id="`contact-url-${index}`"
          data-error="contact-url"
          role="alert"
        >
          Use a lowercase https:// URL.
        </p>
        <label>
          <input
            data-detail-is-hidden
            type="checkbox"
            :checked="detail.isHidden"
            @change="
              changeHidden(
                detail.id,
                ($event.target as HTMLInputElement).checked,
              )
            "
          >
          Hide this detail
        </label>
        <button
          type="button"
          data-action="move-detail-up"
          :disabled="index === 0"
          @click="move(detail.id, -1)"
        >
          Move up
        </button>
        <button
          type="button"
          data-action="move-detail-down"
          :disabled="index === details.length - 1"
          @click="move(detail.id, 1)"
        >
          Move down
        </button>
        <button
          type="button"
          data-action="remove-detail"
          @click="remove(detail.id)"
        >
          Remove detail
        </button>
      </li>
    </ol>
    <button
      type="button"
      data-action="add-detail"
      @click="add"
    >
      Add detail
    </button>
    <p
      v-if="limitError"
      data-error="detail-limit"
      role="alert"
    >
      You can add up to 16 contact details.
    </p>
    <button
      v-if="details !== undefined"
      type="button"
      data-action="unset-details"
      @click="emit('unset')"
    >
      Remove contact list
    </button>
  </fieldset>
</template>
