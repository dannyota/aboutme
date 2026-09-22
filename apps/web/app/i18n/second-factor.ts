// Copy for the pending second-factor page (`/login/second-factor`): the
// passkey and recovery-code completion of an enrolled password or provider
// login, or a settings reauthentication. See
// docs/design/second-factor-authentication.md.
import type { Locale } from './locale';

export type SecondFactorMessage
  = | 'loading'
    | 'unavailable'
    | 'expired'
    | 'signInAgain'
    | 'tryAgain'
    | 'verificationFailed'
    | 'rateLimited'
    | 'passkeyCancelled'
    | 'passkeyUnsupported'
    | 'unknownMethod'
    | 'refresh'
    | 'enterRecoveryCode';

export type SecondFactorCopy = {
  readonly title: string;
  readonly lead: Readonly<Record<'login' | 'reauth', string>>;
  readonly messages: Readonly<Record<SecondFactorMessage, string>>;
  readonly passkey: {
    readonly heading: string;
    readonly description: string;
    readonly button: string;
    readonly pending: string;
  };
  readonly recovery: {
    readonly heading: string;
    readonly description: string;
    readonly label: string;
    readonly button: string;
    readonly pending: string;
  };
};

export const secondFactorCopy: Record<Locale, SecondFactorCopy> = {
  vi: {
    title: 'Xác thực hai bước',
    lead: {
      login: 'Hoàn tất đăng nhập bằng passkey hoặc mã khôi phục.',
      reauth: 'Xác nhận danh tính của bạn để tiếp tục.',
    },
    messages: {
      loading: 'Đang kiểm tra phiên xác thực…',
      unavailable: 'Đã có lỗi. Hãy thử lại.',
      expired: 'Phiên xác thực này đã hết hạn hoặc không còn hiệu lực.',
      signInAgain: 'Đăng nhập lại',
      tryAgain: 'Thao tác không thành công. Hãy thử lại.',
      verificationFailed: 'Xác thực không thành công. Hãy thử lại.',
      rateLimited: 'Bạn đã thử quá nhiều lần. Hãy đợi một chút rồi thử lại.',
      passkeyCancelled: 'Đã hủy xác thực bằng passkey.',
      passkeyUnsupported:
        'Trình duyệt này không hỗ trợ passkey. Hãy dùng mã khôi phục.',
      unknownMethod:
        'Trang này chưa hỗ trợ một trong các phương thức xác thực của bạn. '
        + 'Hãy làm mới trang.',
      refresh: 'Làm mới trang',
      enterRecoveryCode: 'Nhập mã khôi phục.',
    },
    passkey: {
      heading: 'Passkey',
      description: 'Dùng passkey đã lưu trên thiết bị này.',
      button: 'Tiếp tục với passkey',
      pending: 'Đang chờ passkey…',
    },
    recovery: {
      heading: 'Mã khôi phục',
      description: 'Nhập một trong các mã khôi phục của bạn.',
      label: 'Mã khôi phục',
      button: 'Xác thực mã',
      pending: 'Đang xác thực…',
    },
  },
  en: {
    title: 'Two-factor verification',
    lead: {
      login: 'Finish signing in with a passkey or a recovery code.',
      reauth: 'Confirm it\'s you to continue.',
    },
    messages: {
      loading: 'Checking your verification status…',
      unavailable: 'Something went wrong. Try again.',
      expired: 'This verification session has expired or is no longer valid.',
      signInAgain: 'Sign in again',
      tryAgain: 'That did not work. Try again.',
      verificationFailed: 'Verification failed. Try again.',
      rateLimited: 'Too many attempts. Wait a moment and try again.',
      passkeyCancelled: 'Passkey verification was cancelled.',
      passkeyUnsupported:
        'This browser does not support passkeys. Use a recovery code '
        + 'instead.',
      unknownMethod:
        'One of your verification methods is not supported on this page. '
        + 'Refresh to continue.',
      refresh: 'Refresh page',
      enterRecoveryCode: 'Enter your recovery code.',
    },
    passkey: {
      heading: 'Passkey',
      description: 'Use a passkey saved on this device.',
      button: 'Continue with passkey',
      pending: 'Waiting for your passkey…',
    },
    recovery: {
      heading: 'Recovery code',
      description: 'Enter one of your recovery codes.',
      label: 'Recovery code',
      button: 'Verify code',
      pending: 'Verifying…',
    },
  },
};
