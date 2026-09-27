<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';

import {
  isJoinInviteClosed,
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
}

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
    void nextTick(updatePlacement);
  });
});

onBeforeUnmount(() => {
  stopTiming?.();
  window.removeEventListener('resize', updatePlacement);
  window.removeEventListener('keydown', onKeydown);
});

const containerStyle = computed(() => {
  const shared = {
    background: '#FFFFFF',
    border: '1px solid #DCE5F5',
    color: '#56648C',
    fontFamily: '"Be Vietnam Pro", Inter, system-ui, sans-serif',
    zIndex: 40,
  };
  if (placement.value === 'card') {
    return {
      ...shared,
      position: 'fixed' as const,
      right: '16px',
      bottom: '16px',
      width: '320px',
      borderRadius: '14px',
      padding: '16px',
      boxShadow: '0 1px 2px rgba(16, 27, 63, 0.06)',
    };
  }
  return {
    ...shared,
    position: 'fixed' as const,
    left: '0',
    right: '0',
    bottom: '0',
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    padding:
      '12px calc(12px + env(safe-area-inset-right)) '
      + 'calc(12px + env(safe-area-inset-bottom)) '
      + 'calc(12px + env(safe-area-inset-left))',
  };
});
</script>

<template>
  <div
    v-if="visible"
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
            font-size:18px;line-height:1;color:#56648C;cursor:pointer;"
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
        style="margin:0;flex:1;font-size:14px;overflow:hidden;
          text-overflow:ellipsis;white-space:nowrap;"
      >
        {{ bodyText }}
      </p>
      <a
        :href="href"
        style="flex-shrink:0;display:inline-flex;align-items:center;
          height:36px;padding:0 12px;border-radius:10px;background:#1A5CEB;
          color:#FFFFFF;text-decoration:none;font-weight:500;font-size:14px;"
        @click="onFollow"
      >{{ buttonText }}</a>
      <button
        type="button"
        :aria-label="closeLabel"
        style="flex-shrink:0;width:32px;height:32px;border:none;
          background:transparent;font-size:18px;line-height:1;
          color:#56648C;cursor:pointer;"
        @click="close"
      >
        ×
      </button>
    </template>
  </div>
</template>

<style scoped>
@media (prefers-reduced-motion: no-preference) {
  .join-invite {
    animation: join-invite-in 200ms ease-out;
  }
}

@keyframes join-invite-in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
