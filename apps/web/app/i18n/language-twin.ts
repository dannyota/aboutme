import type { TwinLanguage } from '../editor/languageTwin';
import type { WorkspaceCopy } from './workspace';

export type LanguageTwinCopy = {
  readonly action: Readonly<Record<TwinLanguage, string>>;
  readonly languageName: Readonly<Record<TwinLanguage, string>>;
  readonly title: string;
  readonly sentences: Readonly<Record<TwinLanguage, string>>;
  readonly numbers: Readonly<Record<TwinLanguage, string>>;
  readonly unpublished: string;
  readonly photoRemoved: Readonly<Record<TwinLanguage, string>>;
  readonly birthRemoved: string;
  readonly zaloAdded: string;
  readonly zaloFull: string;
  readonly zaloRemoved: string;
  readonly noPhone: string;
  readonly headingsLeft: string;
  readonly agentReady: string;
  readonly agentAbsent: string;
  readonly copyInstructions: string;
  readonly copied: string;
  readonly dismiss: string;
  readonly settings: string;
  readonly agentPrompt: (title: string, language: string) => string;
};

export const languageTwinCopy: WorkspaceCopy<LanguageTwinCopy> = {
  vi: {
    action: {
      vi: 'Tạo bản tiếng Việt',
      en: 'Tạo bản tiếng Anh',
    },
    languageName: { vi: 'Tiếng Việt', en: 'English' },
    title: 'Xem lại bản ngôn ngữ kia',
    sentences: {
      vi: 'Viết lại câu bằng tiếng Việt. Giữ nguyên chức danh '
        + 'và tên công nghệ bằng tiếng Anh.',
      en: 'Viết lại câu bằng tiếng Anh. Đừng dịch từng chữ '
        + 'chức danh và tên công nghệ.',
    },
    numbers: {
      vi: 'Số liệu tiếng Việt dùng 10.000 và 1,9.',
      en: 'Số liệu tiếng Anh dùng 10,000 và 1.9.',
    },
    unpublished: 'Bản này chưa công khai. Hãy xem lại trước khi đăng.',
    photoRemoved: {
      vi: 'Ảnh chưa được sao chép. Thêm lại nếu nhà tuyển dụng '
        + 'trong nước cần ảnh.',
      en: 'Ảnh được bỏ ra. CV tiếng Anh cho công ty sản phẩm '
        + 'hoặc nhà tuyển dụng nước ngoài không dùng ảnh.',
    },
    birthRemoved: 'Ngày sinh đã được bỏ khỏi bản tiếng Anh.',
    zaloAdded: 'Đã thêm Zalo với nội dung “cùng số”, trỏ tới số điện thoại.',
    zaloFull: 'Không thêm được Zalo vì phần liên hệ đã đủ 16 mục.',
    zaloRemoved: 'Gợi ý Zalo đã được bỏ khỏi bản tiếng Anh.',
    noPhone: 'Chưa có số điện thoại để ghi Zalo là cùng số.',
    headingsLeft: 'Một số tiêu đề mục không nằm trong danh sách chuẩn. '
      + 'Hãy đọc lại.',
    agentReady: 'Tác nhân đã kết nối có thể viết lại câu. '
      + 'Tác nhân không thể đăng CV này.',
    agentAbsent: 'Sửa câu tại đây, hoặc kết nối một tác nhân '
      + 'rồi dán hướng dẫn.',
    copyInstructions: 'Sao chép hướng dẫn',
    copied: 'Đã sao chép',
    dismiss: 'Đã xem xong',
    settings: 'Tác nhân đã kết nối',
    agentPrompt: (title, language) =>
      `Viết lại CV "${title}" sang ${language}. `
      + 'Giữ chức danh và tên công nghệ bằng tiếng Anh. '
      + 'Dùng cách viết số của ngôn ngữ đó. '
      + 'Không đăng CV.',
  },
  en: {
    action: {
      vi: 'Create the Vietnamese version',
      en: 'Create the English version',
    },
    languageName: { vi: 'Tiếng Việt', en: 'English' },
    title: 'Review the other language',
    sentences: {
      vi: 'Rewrite the sentences in Vietnamese. '
        + 'Keep English job titles and technology names.',
      en: 'Rewrite the sentences in English. Do not translate '
        + 'job titles and technology names word for word.',
    },
    numbers: {
      vi: 'Vietnamese numbers use 10.000 and 1,9.',
      en: 'English numbers use 10,000 and 1.9.',
    },
    unpublished: 'This copy is not public. Review it before you publish.',
    photoRemoved: {
      vi: 'The photo was not copied. Add it again if a local '
        + 'employer expects one.',
      en: 'The photo was left off. English resumes for product '
        + 'and foreign employers leave it out.',
    },
    birthRemoved: 'The birth date was left off the English copy.',
    zaloAdded: 'A Zalo line was added as “cùng số”, '
      + 'pointing at the phone number.',
    zaloFull: 'Zalo was not added because the contact list '
      + 'already has 16 items.',
    zaloRemoved: 'The Zalo hint was left off the English copy.',
    noPhone: 'There is no phone number to point a Zalo line at.',
    headingsLeft: 'Some section headings are outside the standard list. '
      + 'Read them again.',
    agentReady: 'A connected agent can rewrite the sentences. '
      + 'It cannot publish this copy.',
    agentAbsent: 'Edit the sentences here, or connect an agent '
      + 'and paste the instructions.',
    copyInstructions: 'Copy instructions',
    copied: 'Copied',
    dismiss: 'Done reviewing',
    settings: 'Connected agents',
    agentPrompt: (title, language) =>
      `Rewrite the resume "${title}" into ${language}. `
      + 'Keep English job titles and technology names. '
      + 'Use that language\'s number format. '
      + 'Do not publish the resume.',
  },
};
