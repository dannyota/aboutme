import type { WorkspaceCopy } from './workspace';

type IdentitySettingsCopy = {
  readonly linkedOn: (date: string) => string;
  readonly linked: string;
  readonly unavailableForSignIn: string;
  readonly lastMethod: string;
  readonly unlink: string;
  readonly unlinkTitle: (provider: string) => string;
  readonly unlinkDescription: (provider: string) => string;
  readonly unlinkConfirm: (provider: string) => string;
  readonly cancel: string;
  readonly passwordReauthDescription: (provider: string) => string;
  readonly currentPassword: string;
  readonly continue: string;
  readonly checking: string;
  readonly providerReauthDescription: (provider: string) => string;
  readonly continueWith: (provider: string) => string;
  readonly errors: {
    readonly currentPasswordRequired: string;
    readonly reauthFailed: string;
    readonly lastMethod: string;
    readonly rateLimited: string;
    readonly unavailable: string;
  };
};

export const identitySettingsCopy: WorkspaceCopy<IdentitySettingsCopy> = {
  vi: {
    linkedOn: (date) => `Đã liên kết ngày ${date}`,
    linked: 'Đã liên kết',
    unavailableForSignIn: '. Không dùng để đăng nhập',
    lastMethod:
      'Hãy thêm mật khẩu hoặc liên kết nhà cung cấp khác trước khi '
      + 'hủy liên kết này.',
    unlink: 'Hủy liên kết',
    unlinkTitle: (provider) => `Hủy liên kết ${provider}?`,
    unlinkDescription: (provider) =>
      `Bạn sẽ không thể đăng nhập bằng tài khoản ${provider} này nữa. `
      + 'Các phương thức đăng nhập và thiết bị đã đăng nhập khác '
      + 'vẫn giữ nguyên.',
    unlinkConfirm: (provider) => `Hủy liên kết ${provider}`,
    cancel: 'Hủy',
    passwordReauthDescription: (provider) =>
      `Hãy đăng nhập lại để xác nhận trước khi hủy liên kết ${provider}.`,
    currentPassword: 'Mật khẩu hiện tại',
    continue: 'Tiếp tục',
    checking: 'Đang kiểm tra…',
    providerReauthDescription: (provider) =>
      `Hãy đăng nhập lại với nhà cung cấp, sau đó hủy liên kết ${provider}.`,
    continueWith: (provider) => `Tiếp tục với ${provider}`,
    errors: {
      currentPasswordRequired: 'Nhập mật khẩu hiện tại.',
      reauthFailed: 'Mật khẩu không đúng.',
      lastMethod:
        'Hãy thêm mật khẩu hoặc liên kết nhà cung cấp khác trước khi '
        + 'hủy liên kết này.',
      rateLimited: 'Có quá nhiều lần thử. Hãy thử lại sau.',
      unavailable: 'Đã xảy ra lỗi. Vui lòng thử lại.',
    },
  },
  en: {
    linkedOn: (date) => `Linked on ${date}`,
    linked: 'Linked',
    unavailableForSignIn: '. Not available for sign-in',
    lastMethod:
      'Add a password or link another provider before removing this one.',
    unlink: 'Unlink',
    unlinkTitle: (provider) => `Unlink ${provider}?`,
    unlinkDescription: (provider) =>
      `You will no longer be able to sign in with this ${provider} account. `
      + 'Your other sign-in methods and signed-in devices stay as they are.',
    unlinkConfirm: (provider) => `Unlink ${provider}`,
    cancel: 'Cancel',
    passwordReauthDescription: (provider) =>
      `Sign in again to confirm it’s you before unlinking ${provider}.`,
    currentPassword: 'Current password',
    continue: 'Continue',
    checking: 'Checking…',
    providerReauthDescription: (provider) =>
      `Sign in again with your provider, then unlink ${provider} again.`,
    continueWith: (provider) => `Continue with ${provider}`,
    errors: {
      currentPasswordRequired: 'Enter your current password.',
      reauthFailed: 'Incorrect password.',
      lastMethod:
        'Add a password or link another provider before removing this one.',
      rateLimited: 'Too many attempts. Try again later.',
      unavailable: 'Something went wrong. Please try again.',
    },
  },
};
