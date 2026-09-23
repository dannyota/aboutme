// Copy for the pending second-factor page (`/login/second-factor`): the
// passkey, authenticator-app (TOTP), and recovery-code completion of an
// enrolled password or provider login, or a settings reauthentication. See
// docs/design/second-factor-authentication.md and
// docs/design/totp-second-factor-contract.md.
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
    | 'enterRecoveryCode'
    | 'enterTotpCode'
    | 'totpNotFound';

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
  readonly totp: {
    readonly heading: string;
    readonly description: string;
    readonly guidance: string;
    readonly label: string;
    readonly button: string;
    readonly pending: string;
    /** Localized remaining cool-down time, rounded up to a whole unit; see
     * docs/design/totp-second-factor-contract.md "Per-account TOTP failure
     * budget" for the 15-minute-to-24-hour range this formats. */
    readonly cooldown: (seconds: number) => string;
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
      login:
        'Hoàn tất đăng nhập bằng passkey, ứng dụng xác thực hoặc mã khôi '
        + 'phục.',
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
      enterTotpCode: 'Nhập mã gồm 6 chữ số từ ứng dụng xác thực.',
      totpNotFound:
        'Tài khoản này không còn thiết lập ứng dụng xác thực. Hãy dùng '
        + 'phương thức khác.',
    },
    passkey: {
      heading: 'Passkey',
      description: 'Dùng passkey đã lưu trên thiết bị này.',
      button: 'Tiếp tục với passkey',
      pending: 'Đang chờ passkey…',
    },
    totp: {
      heading: 'Ứng dụng xác thực',
      description: 'Nhập mã 6 chữ số từ ứng dụng xác thực của bạn.',
      guidance:
        'Chỉ nhập mã này trên aboutme.vn. Không chia sẻ mã với bất kỳ ai, '
        + 'kể cả đội ngũ hỗ trợ.',
      label: 'Mã xác thực',
      button: 'Xác thực mã',
      pending: 'Đang xác thực…',
      cooldown: (seconds) => {
        const clamped = Math.max(1, Math.ceil(seconds));
        if (clamped >= 3600) {
          return `Hãy thử lại sau khoảng ${Math.ceil(clamped / 3600)} giờ.`;
        }
        if (clamped >= 60) {
          return `Hãy thử lại sau khoảng ${Math.ceil(clamped / 60)} phút.`;
        }
        return `Hãy thử lại sau khoảng ${clamped} giây.`;
      },
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
      login:
        'Finish signing in with a passkey, an authenticator app, or a '
        + 'recovery code.',
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
      enterTotpCode: 'Enter the 6-digit code from your authenticator app.',
      totpNotFound:
        'This account no longer has an authenticator app set up. Use '
        + 'another method.',
    },
    passkey: {
      heading: 'Passkey',
      description: 'Use a passkey saved on this device.',
      button: 'Continue with passkey',
      pending: 'Waiting for your passkey…',
    },
    totp: {
      heading: 'Authenticator app',
      description: 'Enter the 6-digit code from your authenticator app.',
      guidance:
        'Only enter this code on aboutme.vn. Never share it with anyone, '
        + 'including support.',
      label: 'Authenticator code',
      button: 'Verify code',
      pending: 'Verifying…',
      cooldown: (seconds) => {
        const clamped = Math.max(1, Math.ceil(seconds));
        if (clamped >= 3600) {
          const hours = Math.ceil(clamped / 3600);
          return `Try again in about ${hours} hour${hours === 1 ? '' : 's'}.`;
        }
        if (clamped >= 60) {
          const minutes = Math.ceil(clamped / 60);
          return `Try again in about ${minutes} minute${
            minutes === 1 ? '' : 's'
          }.`;
        }
        return `Try again in about ${clamped} second${
          clamped === 1 ? '' : 's'
        }.`;
      },
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
