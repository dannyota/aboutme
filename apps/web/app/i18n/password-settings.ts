import type {
  PasswordSettingsErrorKind,
} from '../composables/passwordSettings';
import type { PasswordIssue } from '../composables/usePasswordAuth';
import type { WorkspaceCopy } from './workspace';

export type PasswordSettingsMessage
  = | PasswordSettingsErrorKind
    | 'current-password-required'
    | 'passwords-do-not-match'
    | 'new-password-required';

export type PasswordSettingsCopy = {
  readonly title: string;
  readonly hasPassword: string;
  readonly noPassword: string;
  readonly addPassword: string;
  readonly changePassword: string;
  readonly currentPassword: string;
  readonly newPassword: string;
  readonly confirmPassword: string;
  readonly savePassword: string;
  readonly saving: string;
  readonly continue: string;
  readonly checking: string;
  readonly cancel: string;
  readonly providerReauth: string;
  readonly continueWithProvider: (provider: string) => string;
  readonly errors: Readonly<Record<PasswordSettingsMessage, string>>;
  readonly passwordPolicy: Readonly<
    Record<PasswordIssue | 'generic', string>
  >;
  readonly success: {
    readonly added: string;
    readonly changed: string;
  };
};

export const passwordSettingsCopy: WorkspaceCopy<PasswordSettingsCopy> = {
  vi: {
    title: 'Mật khẩu',
    hasPassword: 'Bạn đã đặt mật khẩu.',
    noPassword: 'Chưa đặt mật khẩu.',
    addPassword: 'Thêm mật khẩu',
    changePassword: 'Đổi mật khẩu',
    currentPassword: 'Mật khẩu hiện tại',
    newPassword: 'Mật khẩu mới',
    confirmPassword: 'Nhập lại mật khẩu',
    savePassword: 'Lưu mật khẩu',
    saving: 'Đang lưu…',
    continue: 'Tiếp tục',
    checking: 'Đang kiểm tra…',
    cancel: 'Hủy',
    providerReauth: 'Đăng nhập lại với nhà cung cấp để tiếp tục.',
    continueWithProvider: (provider) => `Tiếp tục với ${provider}`,
    errors: {
      'current-password-required': 'Nhập mật khẩu hiện tại.',
      'passwords-do-not-match': 'Mật khẩu không khớp.',
      'new-password-required': 'Nhập mật khẩu mới.',
      'reauth-failed': 'Mật khẩu không đúng.',
      'reauth-required':
        'Đăng nhập lại để xác nhận danh tính trước khi tiếp tục.',
      'password-invalid': 'Mật khẩu chưa đáp ứng yêu cầu.',
      'rate-limited': 'Bạn đã thử quá nhiều lần. Hãy thử lại sau.',
      'unavailable': 'Đã có lỗi. Hãy thử lại.',
    },
    passwordPolicy: {
      length: 'Mật khẩu phải có ít nhất 12 ký tự.',
      common: 'Mật khẩu này quá phổ biến. Hãy chọn mật khẩu khác.',
      breached:
        'Mật khẩu này đã bị lộ trong một vụ rò rỉ dữ liệu. '
        + 'Hãy chọn mật khẩu khác.',
      generic: 'Mật khẩu chưa đáp ứng yêu cầu.',
    },
    success: {
      added: 'Đã thêm mật khẩu.',
      changed: 'Đã đổi mật khẩu.',
    },
  },
  en: {
    title: 'Password',
    hasPassword: 'You have a password.',
    noPassword: 'No password set.',
    addPassword: 'Add a password',
    changePassword: 'Change password',
    currentPassword: 'Current password',
    newPassword: 'New password',
    confirmPassword: 'Confirm password',
    savePassword: 'Save password',
    saving: 'Saving…',
    continue: 'Continue',
    checking: 'Checking…',
    cancel: 'Cancel',
    providerReauth: 'Sign in again with your provider to continue.',
    continueWithProvider: (provider) => `Continue with ${provider}`,
    errors: {
      'current-password-required': 'Enter your current password.',
      'passwords-do-not-match': 'Passwords do not match.',
      'new-password-required': 'Enter a new password.',
      'reauth-failed': 'Incorrect password.',
      'reauth-required':
        'Sign in again to confirm it\'s you before continuing.',
      'password-invalid': 'Password does not meet our requirements.',
      'rate-limited': 'Too many attempts. Try again later.',
      'unavailable': 'Something went wrong. Please try again.',
    },
    passwordPolicy: {
      length: 'Password must be at least 12 characters.',
      common: 'That password is too common. Choose a different one.',
      breached:
        'That password was exposed in a data breach. Choose a different one.',
      generic: 'Password does not meet our requirements.',
    },
    success: {
      added: 'Password added.',
      changed: 'Password changed.',
    },
  },
};
