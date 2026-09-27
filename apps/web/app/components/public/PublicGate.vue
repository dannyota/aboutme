<script setup lang="ts">
import { computed } from 'vue';

import type {
  PublicGateMessage,
  PublicGateProvider,
} from '../../../server/utils/public-render/envelope';

// Server-rendered only: the gate carries no script and is never hydrated
// (docs/design/viewer-analytics/sign-in-to-view.md#gate). Its text is fixed
// bilingual copy, kept local to this component so the public-render bundle
// never imports an application i18n catalog (docs/design/localization.md).
const props = defineProps<{
  readonly pageTitle: string;
  readonly slug: string;
  readonly lng: 'vi' | 'en';
  readonly providers: readonly PublicGateProvider[];
  readonly message: PublicGateMessage;
  /** `{origin}/`, the home link's href. */
  readonly homeHref: string;
}>();

const vietnamese = computed(() => props.lng === 'vi');

const providerHref = (provider: PublicGateProvider): string =>
  `/api/v1/auth/${provider}/start?purpose=view&slug=${props.slug}`;

const providerLabel = (provider: PublicGateProvider): string => {
  const name = provider === 'google' ? 'Google' : 'LinkedIn';
  return vietnamese.value ? `Tiếp tục với ${name}` : `Continue with ${name}`;
};

// docs/design/viewer-analytics/legal.md#sign-in-gate-text
const gateLead = computed(() => (
  vietnamese.value
    ? 'Đăng nhập để xem CV này.'
    : 'Sign in to view this resume.'
));
const gateRest = computed(() => (
  vietnamese.value
    ? ' Chủ CV yêu cầu người xem đăng nhập để chặn bot và công cụ tự động '
    + 'sao chép nội dung. Nếu bạn tiếp tục, Google hoặc LinkedIn sẽ xác '
    + 'minh tài khoản của bạn cho aboutme. aboutme không lưu tên hay '
    + 'email của bạn và không cho chủ CV biết bạn là ai; chúng tôi chỉ '
    + 'đặt một cookie trên trình duyệt này, cho phép bạn xem CV này '
    + 'trong 7 ngày. Việc đăng nhập không tạo tài khoản aboutme. Chỉ '
    + 'tiếp tục nếu bạn từ 16 tuổi trở lên.'
    : ' The owner asks viewers to sign in, to keep out bots and automated '
      + 'copying. If you continue, Google or LinkedIn confirms your '
      + 'account to aboutme. aboutme does not keep your name or email and '
      + 'does not tell the owner who you are; it only sets a cookie in '
      + 'this browser that lets you view this resume for 7 days. Signing '
      + 'in does not create an aboutme account. Continue only if you are '
      + '16 or older.'
));

const messageText = computed(() => {
  if (props.message === 'cancelled') {
    return vietnamese.value
      ? 'Bạn đã hủy đăng nhập. Chọn một nút ở trên để thử lại.'
      : 'You cancelled sign-in. Choose a button above to try again.';
  }
  if (props.message === 'failed') {
    return vietnamese.value
      ? 'Chúng tôi không thể xác minh đăng nhập của bạn. Vui lòng thử lại.'
      : 'We could not verify your sign-in. Please try again.';
  }
  return '';
});

const refusalText = computed(() => (
  vietnamese.value
    ? 'Không muốn đăng nhập? Hãy liên hệ trực tiếp chủ CV.'
    : 'Prefer not to sign in? Contact the resume owner directly.'
));

const homeLabel = computed(() => (
  vietnamese.value ? 'Về trang chủ aboutme.vn' : 'Back to aboutme.vn'
));
</script>

<template>
  <main id="public-gate">
    <div class="gate-card">
      <h1 class="gate-title">
        {{ pageTitle }}
      </h1>
      <p class="gate-text">
        <strong>{{ gateLead }}</strong>{{ gateRest }}
      </p>
      <div class="gate-providers">
        <a
          v-for="provider in providers"
          :key="provider"
          class="gate-provider"
          :href="providerHref(provider)"
        >{{ providerLabel(provider) }}</a>
      </div>
      <p
        v-if="message !== 'none'"
        class="gate-message"
      >
        {{ messageText }}
      </p>
      <p class="gate-refusal">
        {{ refusalText }}
      </p>
      <a
        class="gate-home"
        :href="homeHref"
      >{{ homeLabel }}</a>
    </div>
  </main>
</template>
