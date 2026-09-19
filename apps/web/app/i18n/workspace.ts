import type { Locale } from './locale';

export type WorkspaceCopy<T extends object> = Readonly<
  Record<Locale, Readonly<T>>
>;

type StateMarkCopy = {
  readonly saved: string;
  readonly unsaved: string;
  readonly saving: string;
  readonly failed: string;
  readonly draft: string;
  readonly publicAt: (link: string) => string;
};

export const workspaceCopy: WorkspaceCopy<StateMarkCopy> = {
  vi: {
    saved: 'Đã lưu',
    unsaved: 'Chưa lưu',
    saving: 'Đang lưu…',
    failed: 'Lưu không thành công',
    draft: 'Bản nháp',
    publicAt: (link) => `Công khai tại aboutme.vn${link}`,
  },
  en: {
    saved: 'Saved',
    unsaved: 'Unsaved',
    saving: 'Saving…',
    failed: 'Save failed',
    draft: 'Draft',
    publicAt: (link) => `Public at aboutme.vn${link}`,
  },
};
