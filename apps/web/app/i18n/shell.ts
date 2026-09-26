// Header copy for the localized routes; every other route uses English.
import type { Locale } from './locale';

export type ShellCopy = {
  readonly primaryNavigation: string;
  readonly signIn: string;
  readonly createAccount: string;
  readonly createResume: string;
  readonly openSource: string;
  readonly templates: string;
  readonly resumes: string;
  readonly views: string;
  readonly settings: string;
  readonly localeLabel: string;
  readonly accountMenu: string;
  readonly logout: string;
  readonly lightMode: string;
  readonly darkMode: string;
  readonly switchToLight: string;
  readonly switchToDark: string;
  readonly lightTheme: string;
  readonly darkTheme: string;
};

export const shellCopy: Record<Locale, ShellCopy> = {
  vi: {
    primaryNavigation: 'Điều hướng chính',
    signIn: 'Đăng nhập',
    createAccount: 'Tạo tài khoản',
    createResume: 'Tạo CV của bạn',
    openSource: 'Mã nguồn mở',
    templates: 'Thư viện',
    resumes: 'CV',
    views: 'Lượt xem',
    settings: 'Cài đặt',
    localeLabel: 'Ngôn ngữ',
    accountMenu: 'Tài khoản',
    logout: 'Đăng xuất',
    lightMode: 'Chế độ sáng',
    darkMode: 'Chế độ tối',
    switchToLight: 'Chuyển sang chế độ sáng',
    switchToDark: 'Chuyển sang chế độ tối',
    lightTheme: 'Chế độ sáng',
    darkTheme: 'Chế độ tối',
  },
  en: {
    primaryNavigation: 'Primary navigation',
    signIn: 'Sign in',
    createAccount: 'Create account',
    createResume: 'Create your resume',
    openSource: 'Open source',
    templates: 'Library',
    resumes: 'Resumes',
    views: 'Views',
    settings: 'Settings',
    localeLabel: 'Language',
    accountMenu: 'Account menu',
    logout: 'Log out',
    lightMode: 'Light mode',
    darkMode: 'Dark mode',
    switchToLight: 'Switch to light theme',
    switchToDark: 'Switch to dark theme',
    lightTheme: 'Light theme',
    darkTheme: 'Dark theme',
  },
};
