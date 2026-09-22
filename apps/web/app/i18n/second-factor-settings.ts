import type {
  SecondFactorSettingsErrorKind,
} from '../composables/secondFactorSettings';
import type { WorkspaceCopy } from './workspace';

export type SecondFactorSettingsMessage
  = | SecondFactorSettingsErrorKind
    | 'cancelled'
    | 'current-password-required'
    | 'reauth-failed';

export type SecondFactorSettingsCopy = {
  readonly title: string;
  readonly description: string;
  readonly unsupported: string;
  readonly emptyTitle: string;
  readonly emptyDescription: string;
  readonly addPasskey: string;
  readonly adding: string;
  readonly created: string;
  readonly lastUsed: string;
  readonly neverUsed: string;
  readonly removePasskey: string;
  readonly removeTitle: string;
  readonly removeDescription: string;
  readonly removeDescriptionFinal: string;
  readonly removeConfirm: string;
  readonly cancel: string;
  readonly close: string;
  readonly passkeyAdded: string;
  readonly passkeyRemoved: string;
  readonly recoveryTitle: string;
  readonly recoveryRemaining: (count: number) => string;
  readonly regenerate: string;
  readonly regenerateTitle: string;
  readonly regenerateDescription: string;
  readonly regenerateConfirm: string;
  readonly revealEnrolledTitle: string;
  readonly revealRegeneratedTitle: string;
  readonly revealDescription: string;
  readonly copyCodes: string;
  readonly copied: string;
  readonly downloadCodes: string;
  readonly closeReveal: string;
  readonly reauthPasswordDescription: string;
  readonly currentPassword: string;
  readonly continueLabel: string;
  readonly checking: string;
  readonly reauthProviderDescription: string;
  readonly continueWithProvider: (provider: string) => string;
  readonly errors: Readonly<Record<SecondFactorSettingsMessage, string>>;
};

export const secondFactorSettingsCopy: WorkspaceCopy<
  SecondFactorSettingsCopy
> = {
  vi: {
    title: 'Passkey',
    description: 'Passkey là lớp xác thực thứ hai gắn với thiết bị của bạn.',
    unsupported: 'Trình duyệt này không hỗ trợ passkey.',
    emptyTitle: 'Chưa có passkey nào.',
    emptyDescription:
      'Thêm passkey để bảo vệ tài khoản bằng một lớp xác thực thứ hai.',
    addPasskey: 'Thêm passkey',
    adding: 'Đang thêm…',
    created: 'Đã tạo ngày',
    lastUsed: 'Dùng lần cuối vào',
    neverUsed: 'Chưa từng dùng',
    removePasskey: 'Xoá',
    removeTitle: 'Xoá passkey',
    removeDescription:
      'Xoá passkey này? Mọi thiết bị và tác nhân đã kết nối khác sẽ bị đăng '
      + 'xuất.',
    removeDescriptionFinal:
      'Đây là passkey cuối cùng của bạn. Xoá nó sẽ tắt xác thực hai lớp và '
      + 'xoá các mã khôi phục. Mọi thiết bị và tác nhân đã kết nối khác sẽ '
      + 'bị đăng xuất.',
    removeConfirm: 'Xoá passkey',
    cancel: 'Hủy',
    close: 'Đóng',
    passkeyAdded: 'Đã thêm passkey.',
    passkeyRemoved: 'Đã xoá passkey.',
    recoveryTitle: 'Mã khôi phục',
    recoveryRemaining: (count) => `Còn lại ${count} mã khôi phục.`,
    regenerate: 'Tạo lại mã khôi phục',
    regenerateTitle: 'Tạo lại mã khôi phục',
    regenerateDescription:
      'Tạo lại sẽ vô hiệu các mã khôi phục cũ và đăng xuất mọi thiết bị, '
      + 'tác nhân đã kết nối khác.',
    regenerateConfirm: 'Tạo lại',
    revealEnrolledTitle: 'Lưu mã khôi phục của bạn',
    revealRegeneratedTitle: 'Mã khôi phục mới của bạn',
    revealDescription:
      'Mỗi mã chỉ dùng được một lần. Đây là lần duy nhất mã được hiển thị; '
      + 'hãy lưu lại trước khi đóng.',
    copyCodes: 'Sao chép',
    copied: 'Đã sao chép.',
    downloadCodes: 'Tải xuống',
    closeReveal: 'Tôi đã lưu các mã này',
    reauthPasswordDescription: 'Nhập mật khẩu hiện tại để xác nhận danh tính.',
    currentPassword: 'Mật khẩu hiện tại',
    continueLabel: 'Tiếp tục',
    checking: 'Đang kiểm tra…',
    reauthProviderDescription:
      'Đăng nhập lại với nhà cung cấp để xác nhận danh tính.',
    continueWithProvider: (provider) => `Tiếp tục với ${provider}`,
    errors: {
      'reauth-required':
        'Đăng nhập lại để xác nhận danh tính trước khi tiếp tục.',
      'enrollment-closed': 'Việc thêm passkey mới hiện đang tắt.',
      'limit-reached': 'Bạn đã đạt số passkey tối đa.',
      'challenge-invalid': 'Yêu cầu đã hết hạn. Hãy thử lại.',
      'verification-failed': 'Không thể xác minh passkey đó. Hãy thử lại.',
      'not-found': 'Không tìm thấy passkey đó.',
      'rate-limited': 'Bạn đã thử quá nhiều lần. Hãy thử lại sau.',
      'unavailable': 'Đã có lỗi. Hãy thử lại.',
      'cancelled': 'Đã hủy.',
      'current-password-required': 'Nhập mật khẩu hiện tại.',
      'reauth-failed': 'Mật khẩu không đúng.',
    },
  },
  en: {
    title: 'Passkeys',
    description: 'Passkeys are a second factor bound to your device.',
    unsupported: 'This browser doesn\'t support passkeys.',
    emptyTitle: 'No passkeys yet.',
    emptyDescription:
      'Add a passkey to protect your account with a second factor.',
    addPasskey: 'Add a passkey',
    adding: 'Adding…',
    created: 'Created',
    lastUsed: 'Last used',
    neverUsed: 'Last used Never',
    removePasskey: 'Remove',
    removeTitle: 'Remove passkey',
    removeDescription:
      'Remove this passkey? Every other device and connected agent will be '
      + 'signed out.',
    removeDescriptionFinal:
      'This is your last passkey. Removing it turns off two-factor sign-in '
      + 'and deletes your recovery codes. Every other device and connected '
      + 'agent will be signed out.',
    removeConfirm: 'Remove passkey',
    cancel: 'Cancel',
    close: 'Close',
    passkeyAdded: 'Passkey added.',
    passkeyRemoved: 'Passkey removed.',
    recoveryTitle: 'Recovery codes',
    recoveryRemaining: (count) => `${count} recovery codes remaining.`,
    regenerate: 'Regenerate recovery codes',
    regenerateTitle: 'Regenerate recovery codes',
    regenerateDescription:
      'Regenerating invalidates your old recovery codes and signs out every '
      + 'other device and connected agent.',
    regenerateConfirm: 'Regenerate',
    revealEnrolledTitle: 'Save your recovery codes',
    revealRegeneratedTitle: 'Your new recovery codes',
    revealDescription:
      'Each code works once. This is the only time they are shown, so save '
      + 'them before closing.',
    copyCodes: 'Copy',
    copied: 'Copied.',
    downloadCodes: 'Download',
    closeReveal: 'I\'ve saved these codes',
    reauthPasswordDescription:
      'Enter your current password to confirm it\'s you.',
    currentPassword: 'Current password',
    continueLabel: 'Continue',
    checking: 'Checking…',
    reauthProviderDescription:
      'Sign in again with your provider to confirm it\'s you.',
    continueWithProvider: (provider) => `Continue with ${provider}`,
    errors: {
      'reauth-required':
        'Sign in again to confirm it\'s you before continuing.',
      'enrollment-closed': 'Adding a new passkey is turned off right now.',
      'limit-reached': 'You have reached the passkey limit.',
      'challenge-invalid': 'That request expired. Try again.',
      'verification-failed': 'That passkey could not be verified. Try again.',
      'not-found': 'That passkey could not be found.',
      'rate-limited': 'Too many attempts. Try again later.',
      'unavailable': 'Something went wrong. Please try again.',
      'cancelled': 'That was cancelled.',
      'current-password-required': 'Enter your current password.',
      'reauth-failed': 'Incorrect password.',
    },
  },
};
