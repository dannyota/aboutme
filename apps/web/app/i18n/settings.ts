import type { WorkspaceCopy } from './workspace';

export type SettingsCopy = {
  readonly title: string;
  readonly devices: string;
  readonly unknownDevice: string;
  readonly userAgent: {
    readonly unknown: string;
    readonly on: (browser: string, system: string) => string;
  };
  readonly currentDevice: string;
  readonly lastSeen: (time: string) => string;
  readonly logOut: string;
  readonly revoke: string;
  readonly logOutEverywhere: string;
  readonly revokeFailed: string;
  readonly logOutEverywhereFailed: string;
  readonly providers: string;
  readonly genericError: string;
  readonly reauthLink: string;
  readonly reauthAction: string;
  readonly signInAgainWith: (provider: string) => string;
  readonly cancelled: string;
  readonly identityAlreadyLinked: string;
  readonly unlinked: (provider: string) => string;
  readonly sessionsRemain: string;
  readonly signOutOtherDevices: string;
  readonly addProvider: string;
  readonly linkProvider: (provider: string) => string;
  readonly secondFactorEndsOtherSessions: string;
};

export const settingsCopy: WorkspaceCopy<SettingsCopy> = {
  vi: {
    title: 'Cài đặt',
    devices: 'Thiết bị đã đăng nhập',
    unknownDevice: 'Thiết bị không xác định',
    userAgent: {
      unknown: 'Trình duyệt không xác định',
      on: (browser, system) => `${browser} trên ${system}`,
    },
    currentDevice: 'Thiết bị này',
    lastSeen: (time) => `Hoạt động lần cuối ${time}`,
    logOut: 'Đăng xuất',
    revoke: 'Thu hồi',
    logOutEverywhere: 'Đăng xuất khỏi mọi nơi',
    revokeFailed: 'Không thể thu hồi phiên đó. Hãy thử lại.',
    logOutEverywhereFailed: 'Không thể đăng xuất khỏi mọi nơi. Hãy thử lại.',
    providers: 'Phương thức đăng nhập',
    genericError: 'Đã xảy ra lỗi. Hãy thử lại.',
    reauthLink:
      'Hãy đăng nhập lại để xác nhận trước khi liên kết nhà cung cấp mới.',
    reauthAction: 'Hãy đăng nhập lại để xác nhận, rồi thử lại.',
    signInAgainWith: (provider) => `Đăng nhập lại với ${provider}`,
    cancelled: 'Đã hủy.',
    identityAlreadyLinked:
      'Nhà cung cấp đó đã liên kết với một tài khoản aboutme.vn khác.',
    unlinked: (provider) => `Đã hủy liên kết ${provider}.`,
    sessionsRemain: 'Các thiết bị đã đăng nhập vẫn giữ nguyên.',
    signOutOtherDevices: 'Đăng xuất các thiết bị khác',
    addProvider: 'Thêm phương thức đăng nhập khác',
    linkProvider: (provider) => `Liên kết ${provider}`,
    secondFactorEndsOtherSessions:
      'Thêm, thay, hoặc xoá passkey hay ứng dụng xác thực, hoặc tạo lại mã '
      + 'khôi phục, sẽ đăng xuất mọi thiết bị và tác nhân đã kết nối khác. '
      + 'Thiết bị này vẫn giữ đăng nhập.',
  },
  en: {
    title: 'Settings',
    devices: 'Signed-in devices',
    unknownDevice: 'Unknown device',
    userAgent: {
      unknown: 'Unknown browser',
      on: (browser, system) => `${browser} on ${system}`,
    },
    currentDevice: 'This device',
    lastSeen: (time) => `Last seen ${time}`,
    logOut: 'Log out',
    revoke: 'Revoke',
    logOutEverywhere: 'Log out everywhere',
    revokeFailed: 'Could not revoke that session. Try again.',
    logOutEverywhereFailed: 'Could not log out everywhere. Try again.',
    providers: 'Sign-in providers',
    genericError: 'Something went wrong. Please try again.',
    reauthLink:
      'Sign in again to confirm it\'s you before we link a new provider.',
    reauthAction: 'Sign in again to confirm it\'s you, then try again.',
    signInAgainWith: (provider) => `Sign in again with ${provider}`,
    cancelled: 'That was cancelled.',
    identityAlreadyLinked:
      'That provider is already linked to a different aboutme.vn account.',
    unlinked: (provider) => `${provider} is unlinked.`,
    sessionsRemain: 'Devices that are already signed in stay signed in.',
    signOutOtherDevices: 'Sign out other devices',
    addProvider: 'Add another sign-in provider',
    linkProvider: (provider) => `Link ${provider}`,
    secondFactorEndsOtherSessions:
      'Adding, replacing, or removing a passkey or authenticator app, or '
      + 'regenerating recovery codes, signs out every other device and '
      + 'connected agent. This device stays signed in.',
  },
};
