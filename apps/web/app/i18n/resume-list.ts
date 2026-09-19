import type { WorkspaceCopy } from './workspace';

export type ResumeListCopy = {
  readonly waitingAuth: string;
  readonly loading: string;
  readonly unavailable: string;
  readonly title: string;
  readonly create: string;
  readonly listLabel: string;
  readonly updated: (time: string) => string;
  readonly moreActions: (title: string) => string;
  readonly rename: (title: string) => string;
  readonly delete: (title: string) => string;
  readonly renameAction: string;
  readonly deleteAction: string;
  readonly empty: string;
  readonly emptyHelp: string;
  readonly emptySlot: string;
  readonly resumeCap: string;
  readonly createFailed: string;
  readonly retryLater: string;
  readonly sessionLost: string;
  readonly resumeChanged: string;
  readonly renameTitle: string;
  readonly renameDescription: string;
  readonly renameFieldLabel: string;
  readonly save: string;
  readonly deleteTitle: string;
  readonly deleteDescriptionGeneric: string;
  readonly deleteDescription: (title: string) => string;
  readonly cancel: string;
  readonly close: string;
  readonly confirmDelete: string;
  readonly deleteInput: string;
  readonly relativeTime: {
    readonly justNow: string;
    readonly minutes: (count: number) => string;
    readonly hours: (count: number) => string;
    readonly days: (count: number) => string;
    readonly date: (day: number, month: number, year: number) => string;
  };
};

export const resumeListCopy: WorkspaceCopy<ResumeListCopy> = {
  vi: {
    waitingAuth: 'Đang kiểm tra phiên đăng nhập.',
    loading: 'Đang tải CV.',
    unavailable: 'CV hiện không khả dụng. Hãy thử lại.',
    title: 'CV',
    create: 'Tạo CV',
    listLabel: 'CV của bạn',
    updated: (time) => `Cập nhật ${time}`,
    moreActions: (title) => `Thao tác khác cho ${title}`,
    rename: (title) => `Đổi tên ${title}`,
    delete: (title) => `Xóa ${title}`,
    renameAction: 'Đổi tên',
    deleteAction: 'Xóa',
    empty: 'Chưa có CV.',
    emptyHelp: 'Dùng Tạo CV để bắt đầu. Bạn có thể giữ tối đa ba CV.',
    emptySlot: 'Chỗ trống',
    resumeCap: 'Bạn đã đạt giới hạn số CV.',
    createFailed: 'Không thể tạo CV. Hãy thử lại.',
    retryLater: 'Hãy chờ rồi thử lại.',
    sessionLost: 'Phiên đăng nhập đã kết thúc. Hãy đăng nhập lại.',
    resumeChanged:
      'CV này đã thay đổi. Hãy mở lại phần xóa và xác nhận tên mới.',
    renameTitle: 'Đổi tên CV',
    renameDescription: 'Nhập tên CV mới.',
    renameFieldLabel: 'Tên CV',
    save: 'Lưu',
    deleteTitle: 'Xóa CV',
    deleteDescriptionGeneric: 'Thao tác này sẽ xóa CV vĩnh viễn.',
    deleteDescription: (title) =>
      `Xóa “${title}”? Thao tác này sẽ xóa CV vĩnh viễn.`,
    cancel: 'Hủy',
    close: 'Đóng',
    confirmDelete: 'Xóa',
    deleteInput: 'Nhập DELETE để xác nhận',
    relativeTime: {
      justNow: 'vừa xong',
      minutes: (count) => `${count} phút trước`,
      hours: (count) => `${count} giờ trước`,
      days: (count) => `${count} ngày trước`,
      date: (day, month, year) => `${day} thg ${month} ${year}`,
    },
  },
  en: {
    waitingAuth: 'Checking your session.',
    loading: 'Loading resumes.',
    unavailable: 'Resumes are unavailable. Try again.',
    title: 'Resumes',
    create: 'Create resume',
    listLabel: 'Your resumes',
    updated: (time) => `Updated ${time}`,
    moreActions: (title) => `More actions for ${title}`,
    rename: (title) => `Rename ${title}`,
    delete: (title) => `Delete ${title}`,
    renameAction: 'Rename',
    deleteAction: 'Delete',
    empty: 'No resumes yet.',
    emptyHelp: 'Use Create resume to start one. You can keep up to three.',
    emptySlot: 'Empty slot',
    resumeCap: 'You have reached the resume limit.',
    createFailed: 'Could not create the resume. Try again.',
    retryLater: 'Please wait, then try again.',
    sessionLost: 'Your session ended. Sign in again.',
    resumeChanged:
      'This resume changed. Reopen deletion and confirm its current title.',
    renameTitle: 'Rename resume',
    renameDescription: 'Enter the new resume title.',
    renameFieldLabel: 'Title',
    save: 'Save',
    deleteTitle: 'Delete resume',
    deleteDescriptionGeneric: 'This permanently deletes the resume.',
    deleteDescription: (title) =>
      `Delete "${title}"? This permanently deletes the resume.`,
    cancel: 'Cancel',
    close: 'Close',
    confirmDelete: 'Delete',
    deleteInput: 'Type DELETE to confirm',
    relativeTime: {
      justNow: 'just now',
      minutes: (count) => `${count} minute${count === 1 ? '' : 's'} ago`,
      hours: (count) => `${count} hour${count === 1 ? '' : 's'} ago`,
      days: (count) => `${count} day${count === 1 ? '' : 's'} ago`,
      date: (day, month, year) => `${day} ${[
        'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
        'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
      ][month - 1]} ${year}`,
    },
  },
};
