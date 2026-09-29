<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from 'vue';

import {
  isJoinInviteClosed,
  JOIN_INVITE_BAR_HEIGHT,
  JOIN_INVITE_BAR_TEXT_MIN_WIDTH,
  JOIN_INVITE_ENTRANCE,
  joinInviteContainerStyle,
  joinInvitePlacement,
  recordJoinInviteClosed,
  startJoinInviteTiming,
  type JoinInvitePlacement,
} from '../../public/joinInvite';

// docs/design/viewer-analytics/sign-in-to-view.md#join-invite and
// docs/design/public-page-theme.md (bar token values, "Join invite").
const props = defineProps<{
  readonly lng: 'vi' | 'en';
  /** The `data-join-invite` marker value: `/register` or `/login`. */
  readonly href: string;
  /** The public resume's root element, measured for placement. */
  readonly root: HTMLElement;
}>();

const visible = ref(false);
const placement = ref<JoinInvitePlacement>('bar');
const viewportWidth = ref(0);
const container = ref<HTMLElement>();
let stopTiming: (() => void) | null = null;

const vietnamese = computed(() => props.lng === 'vi');
const bodyText = computed(() => (
  vietnamese.value
    ? 'Tự tạo CV của bạn trên aboutme.vn: miễn phí, mã nguồn mở, chia sẻ '
    + 'bằng một đường dẫn.'
    : 'Make your own resume on aboutme.vn: free, open source, shared with '
      + 'one link.'
));
const buttonText = computed(() => (
  vietnamese.value ? 'Tạo CV miễn phí' : 'Create a free resume'
));
const closeLabel = computed(() => (vietnamese.value ? 'Đóng' : 'Close'));
const regionLabel = computed(() => (
  vietnamese.value
    ? 'Lời mời tạo CV miễn phí'
    : 'Create a free resume invite'
));

function updatePlacement(): void {
  placement.value = joinInvitePlacement(props.root);
  viewportWidth.value = window.innerWidth;
}

// Below 360 px the bar keeps only the button and the close button. This is
// script, not a media query: the public page loads no stylesheet for the
// invite.
const showBarText = computed(
  () => viewportWidth.value >= JOIN_INVITE_BAR_TEXT_MIN_WIDTH,
);

// A 200 ms fade and 8 px slide from the bottom edge, skipped entirely under
// `prefers-reduced-motion: reduce`.
function animateEntrance(): void {
  const element = container.value;
  if (element === undefined || typeof element.animate !== 'function') return;
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
  element.animate(
    [...JOIN_INVITE_ENTRANCE.keyframes],
    { ...JOIN_INVITE_ENTRANCE.options },
  );
}

// The bar never covers resume text: while it shows, the page gains bottom
// padding equal to the bar's total height.
let restorePadding: (() => void) | null = null;

function syncPagePadding(): void {
  restorePadding?.();
  restorePadding = null;
  if (!visible.value || placement.value !== 'bar') return;
  const style = document.body.style;
  const previous = style.getPropertyValue('padding-bottom');
  style.setProperty('padding-bottom', JOIN_INVITE_BAR_HEIGHT);
  restorePadding = () => {
    if (previous === '') style.removeProperty('padding-bottom');
    else style.setProperty('padding-bottom', previous);
  };
}

watch([visible, placement], syncPagePadding);

function close(): void {
  recordJoinInviteClosed();
  visible.value = false;
}

function onFollow(): void {
  recordJoinInviteClosed();
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && visible.value) close();
}

onMounted(() => {
  if (isJoinInviteClosed()) return;
  updatePlacement();
  window.addEventListener('resize', updatePlacement, { passive: true });
  window.addEventListener('keydown', onKeydown);
  stopTiming = startJoinInviteTiming(() => {
    visible.value = true;
    void nextTick(() => {
      updatePlacement();
      animateEntrance();
    });
  });
});

onBeforeUnmount(() => {
  stopTiming?.();
  restorePadding?.();
  restorePadding = null;
  window.removeEventListener('resize', updatePlacement);
  window.removeEventListener('keydown', onKeydown);
});

const containerStyle = computed(
  () => joinInviteContainerStyle(placement.value),
);
</script>

<template>
  <div
    v-if="visible"
    ref="container"
    role="region"
    :aria-label="regionLabel"
    class="join-invite"
    data-join-invite-placement=""
    :data-placement="placement"
    :style="containerStyle"
  >
    <template v-if="placement === 'card'">
      <div style="display:flex;justify-content:flex-end;">
        <button
          type="button"
          :aria-label="closeLabel"
          style="width:32px;height:32px;border:none;background:transparent;
            font-size:18px;line-height:1;color:#5C6178;cursor:pointer;"
          @click="close"
        >
          ×
        </button>
      </div>
      <p style="margin:0 0 12px;font-size:14px;line-height:1.5;">
        {{ bodyText }}
      </p>
      <a
        :href="href"
        style="display:block;width:100%;height:40px;line-height:40px;
          text-align:center;border-radius:10px;background:#1A5CEB;
          color:#FFFFFF;text-decoration:none;font-weight:500;
          box-sizing:border-box;"
        @click="onFollow"
      >{{ buttonText }}</a>
    </template>
    <template v-else>
      <p
        v-if="showBarText"
        class="join-invite-text"
        style="margin:0;flex:1;min-width:0;font-size:14px;overflow:hidden;
          text-overflow:ellipsis;white-space:nowrap;"
      >
        {{ bodyText }}
      </p>
      <a
        :href="href"
        style="flex-shrink:0;display:inline-flex;align-items:center;
          margin-left:auto;height:36px;padding:0 12px;border-radius:10px;
          background:#1A5CEB;
          color:#FFFFFF;text-decoration:none;font-weight:500;font-size:14px;"
        @click="onFollow"
      >{{ buttonText }}</a>
      <button
        type="button"
        :aria-label="closeLabel"
        style="flex-shrink:0;width:32px;height:32px;border:none;
          background:transparent;font-size:18px;line-height:1;
          color:#5C6178;cursor:pointer;"
        @click="close"
      >
        ×
      </button>
    </template>
  </div>
</template>
