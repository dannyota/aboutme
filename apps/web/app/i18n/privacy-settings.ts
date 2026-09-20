import type {
  AccountExportErrorKind,
  PrivacySettingsErrorKind,
} from '../composables/privacySettings';
import type { WorkspaceCopy } from './workspace';

export type PrivacySettingsReauthErrorKind
  = | PrivacySettingsErrorKind
    | 'current-password-required';

export type PrivacySettingsCopy = {
  readonly localeLabel: string;
  readonly title: string;
  readonly exportTitle: string;
  readonly exportDescription: string;
  readonly exportAction: string;
  readonly exportPending: string;
  readonly exportError: (kind: AccountExportErrorKind) => string;
  readonly deleteTitle: string;
  readonly deleteDescription: string;
  readonly deleteAction: string;
  readonly deleteDialogTitle: string;
  readonly deletionDisclosure: string;
  readonly deleteConfirmInput: string;
  readonly deleteError: (kind: PrivacySettingsReauthErrorKind) => string;
  readonly passwordReauthDescription: string;
  readonly currentPassword: string;
  readonly providerReauthDescription: string;
  readonly continue: string;
  readonly checking: string;
  readonly cancel: string;
  readonly reauthError: (kind: PrivacySettingsReauthErrorKind) => string;
  readonly providerContinue: (provider: string) => string;
};

export const privacySettingsCopy: WorkspaceCopy<PrivacySettingsCopy> = {
  vi: {
    localeLabel: 'Ngôn ngữ',
    title: 'Quyền riêng tư',
    exportTitle: 'Tải dữ liệu của bạn',
    exportDescription: 'Tải bản sao JSON của tài khoản và CV của bạn.',
    exportAction: 'Tải dữ liệu của bạn',
    exportPending: 'Đang chuẩn bị tệp tải xuống…',
    exportError: (kind) => ({
      'session-required': 'Phiên đăng nhập đã kết thúc. Hãy đăng nhập lại.',
      'temporarily-unavailable':
        'Không thể xuất dữ liệu lúc này. Hãy thử lại.',
      'unavailable': 'Không thể xuất dữ liệu của bạn. Hãy thử lại.',
    })[kind],
    deleteTitle: 'Xóa tài khoản',
    deleteDescription: 'Thao tác này xóa vĩnh viễn tài khoản và CV của bạn.',
    deleteAction: 'Xóa tài khoản của tôi',
    deleteDialogTitle: 'Xóa tài khoản của bạn?',
    deletionDisclosure: [
      'Quyền truy cập kết thúc ngay.',
      'Mục tiêu là xóa tệp riêng tư trong vòng 24 giờ.',
      'Bản sao lưu hết hạn theo lịch 30 ngày.',
    ].join(' '),
    deleteConfirmInput: 'Nhập DELETE để xóa vĩnh viễn tài khoản của bạn',
    deleteError: (kind) => ({
      'session-required': 'Phiên đăng nhập đã kết thúc. Hãy đăng nhập lại.',
      'account-changed':
        'Tài khoản của bạn đã thay đổi. Hãy xem lại rồi thử lại.',
      'rate-limited': 'Bạn đã thử quá nhiều lần. Hãy thử lại sau.',
      'reauth-required':
        'Không thể xóa tài khoản lúc này. Hãy thử lại.',
      'reauth-failed': 'Không thể xóa tài khoản lúc này. Hãy thử lại.',
      'unavailable': 'Không thể xóa tài khoản lúc này. Hãy thử lại.',
      'current-password-required':
        'Không thể xóa tài khoản lúc này. Hãy thử lại.',
    })[kind],
    passwordReauthDescription:
      'Hãy đăng nhập lại để xác nhận danh tính trước khi xóa tài khoản.',
    currentPassword: 'Mật khẩu hiện tại',
    providerReauthDescription: [
      'Hãy đăng nhập lại bằng nhà cung cấp của bạn,',
      'rồi xác nhận xóa tài khoản.',
    ].join(' '),
    continue: 'Tiếp tục',
    checking: 'Đang kiểm tra…',
    cancel: 'Hủy',
    reauthError: (kind) => ({
      'reauth-failed': 'Mật khẩu không đúng.',
      'rate-limited': 'Bạn đã thử quá nhiều lần. Hãy thử lại sau.',
      'session-required': 'Phiên đăng nhập đã kết thúc. Hãy đăng nhập lại.',
      'reauth-required': 'Đã xảy ra lỗi. Hãy thử lại.',
      'account-changed': 'Đã xảy ra lỗi. Hãy thử lại.',
      'unavailable': 'Đã xảy ra lỗi. Hãy thử lại.',
      'current-password-required': 'Nhập mật khẩu hiện tại của bạn.',
    })[kind],
    providerContinue: (provider) => `Tiếp tục với ${provider}`,
  },
  en: {
    localeLabel: 'Language',
    title: 'Privacy',
    exportTitle: 'Download your data',
    exportDescription: 'Download a JSON copy of your account and resumes.',
    exportAction: 'Download your data',
    exportPending: 'Preparing download…',
    exportError: (kind) => ({
      'session-required': 'Your session ended. Sign in again.',
      'temporarily-unavailable':
        'Your export is temporarily unavailable. Try again.',
      'unavailable': 'Could not export your data. Try again.',
    })[kind],
    deleteTitle: 'Delete account',
    deleteDescription: 'This permanently deletes your account and resumes.',
    deleteAction: 'Delete my account',
    deleteDialogTitle: 'Delete your account?',
    deletionDisclosure: [
      'Access ends immediately.',
      'Private-media removal targets 24 hours.',
      'Backup copies expire on the 30-day schedule.',
    ].join(' '),
    deleteConfirmInput: 'Type DELETE to permanently delete your account',
    deleteError: (kind) => ({
      'session-required': 'Your session ended. Sign in again.',
      'account-changed': 'Your account changed. Review it and try again.',
      'rate-limited': 'Too many attempts. Try again later.',
      'reauth-required':
        'Account deletion is temporarily unavailable. Try again.',
      'reauth-failed':
        'Account deletion is temporarily unavailable. Try again.',
      'unavailable':
        'Account deletion is temporarily unavailable. Try again.',
      'current-password-required':
        'Account deletion is temporarily unavailable. Try again.',
    })[kind],
    passwordReauthDescription:
      'Sign in again to confirm it’s you before deleting your account.',
    currentPassword: 'Current password',
    providerReauthDescription:
      'Sign in again with your provider, then confirm deletion again.',
    continue: 'Continue',
    checking: 'Checking…',
    cancel: 'Cancel',
    reauthError: (kind) => ({
      'reauth-failed': 'Incorrect password.',
      'rate-limited': 'Too many attempts. Try again later.',
      'session-required': 'Your session ended. Sign in again.',
      'reauth-required': 'Something went wrong. Please try again.',
      'account-changed': 'Something went wrong. Please try again.',
      'unavailable': 'Something went wrong. Please try again.',
      'current-password-required': 'Enter your current password.',
    })[kind],
    providerContinue: (provider) => `Continue with ${provider}`,
  },
};
