// Header copy for the localized routes; every other route uses English.
import type { Locale } from './locale';

export type ShellCopy = {
  readonly signIn: string;
  readonly createAccount: string;
  readonly templates: string;
  readonly resumes: string;
  readonly settings: string;
  readonly localeLabel: string;
  readonly lightMode: string;
  readonly darkMode: string;
  readonly switchToLight: string;
  readonly switchToDark: string;
};

export const shellCopy: Record<Locale, ShellCopy> = {
  vi: {
    signIn: 'Đăng nhập',
    createAccount: 'Tạo tài khoản',
    templates: 'Mẫu',
    resumes: 'CV',
    settings: 'Cài đặt',
    localeLabel: 'Ngôn ngữ',
    lightMode: 'Chế độ sáng',
    darkMode: 'Chế độ tối',
    switchToLight: 'Chuyển sang chế độ sáng',
    switchToDark: 'Chuyển sang chế độ tối',
  },
  en: {
    signIn: 'Sign in',
    createAccount: 'Create account',
    templates: 'Templates',
    resumes: 'Resumes',
    settings: 'Settings',
    localeLabel: 'Language',
    lightMode: 'Light mode',
    darkMode: 'Dark mode',
    switchToLight: 'Switch to light theme',
    switchToDark: 'Switch to dark theme',
  },
};
