import type { WorkspaceCopy } from './workspace';

export type EditorHistoryCopy = {
  readonly lead: string;
  readonly count: (n: number) => string;
  readonly countName: (n: number) => string;
  readonly listLabel: string;
  readonly current: string;
  readonly currentDetail: string;
  readonly causeEdit: string;
  readonly causeAssistant: (name: string) => string;
  readonly causeAssistantUnnamed: string;
  readonly causeRestore: string;
  readonly causeAccept: string;
  readonly retention: string;
  readonly emptyTitle: string;
  readonly emptyBody: string;
  readonly loading: string;
  readonly loadFailed: string;
  readonly versionTitle: (time: string) => string;
  readonly viewOnly: string;
  readonly previewLabel: (time: string) => string;
  readonly showing: (time: string) => string;
  readonly previewLoading: string;
  readonly previewFailed: string;
  readonly versionGone: string;
  readonly backToEditing: string;
  readonly restore: string;
  readonly restoreBlocked: string;
  readonly deleteVersionLabel: string;
  readonly restoreTitle: (time: string) => string;
  readonly restorePublic: (link: string) => string;
  readonly restorePrivate: string;
  readonly restoreWhat: (time: string) => string;
  readonly restoreNewTitle: (title: string) => string;
  readonly restoreNewLanguage: (language: string) => string;
  readonly restoreKept: string;
  readonly restoreUndoNote: string;
  readonly cancel: string;
  readonly restoring: string;
  readonly restoreChanged: string;
  readonly restoreInvalid: string;
  readonly restoreNetwork: string;
  readonly restoreBusy: string;
  readonly restoreFailed: string;
  readonly restoredPrivate: (time: string) => string;
  readonly restoredPublic: (time: string, link: string) => string;
  readonly undo: string;
  readonly deleteTitle: (time: string) => string;
  readonly deleteBody: string;
  readonly deleteConfirm: string;
  readonly deleting: string;
  readonly clearing: string;
  readonly deleteFailed: string;
  readonly deleted: (time: string) => string;
  readonly clear: string;
  readonly clearConfirm: string;
  readonly clearTitle: string;
  readonly clearBody: (n: number) => string;
  readonly clearFailed: string;
  readonly cleared: string;
};

export const editorHistoryCopy: WorkspaceCopy<EditorHistoryCopy> = {
  vi: {
    lead: 'Mỗi phiên bản là CV ngay trước một lượt chỉnh sửa.',
    count: (n) => `${n} / 50`,
    countName: (n) => `${n} trên tối đa 50 phiên bản`,
    listLabel: 'Các phiên bản của CV',
    current: 'Phiên bản hiện tại',
    currentDetail: 'CV như trong trình chỉnh sửa',
    causeEdit: 'Trước khi bạn chỉnh sửa',
    causeAssistant: (name) => `Trước khi trợ lý ${name} chỉnh sửa`,
    causeAssistantUnnamed: 'Trước khi một trợ lý AI chỉnh sửa',
    causeRestore: 'Trước khi bạn khôi phục một phiên bản',
    causeAccept: 'Trước khi bạn chấp nhận thay đổi được đề xuất',
    retention:
      'Lịch sử giữ tối đa 50 phiên bản trong 30 ngày. Khi đầy, phiên bản '
      + 'cũ nhất tự động bị xóa.',
    emptyTitle: 'Chưa có phiên bản nào',
    emptyBody:
      'Khi bạn hoặc trợ lý AI bắt đầu chỉnh sửa, CV trước lượt đó được lưu '
      + 'ở đây trong 30 ngày.',
    loading: 'Đang tải lịch sử',
    loadFailed: 'Không tải được lịch sử.',
    versionTitle: (time) => `Phiên bản lúc ${time}`,
    viewOnly: 'Chỉ xem. Khôi phục sẽ giữ ảnh hiện tại.',
    previewLabel: (time) => `Bản xem trước phiên bản lúc ${time}`,
    showing: (time) => `Đang xem phiên bản lúc ${time}`,
    previewLoading: 'Đang tải phiên bản',
    previewFailed: 'Không tải được phiên bản này.',
    versionGone:
      'Phiên bản này không còn trong lịch sử. Có thể nó đã quá 30 ngày hoặc '
      + 'đã bị xóa.',
    backToEditing: 'Quay lại chỉnh sửa',
    restore: 'Khôi phục',
    restoreBlocked: 'Bạn có thể khôi phục khi mọi thay đổi đã được lưu.',
    deleteVersionLabel: 'Xóa phiên bản này',
    restoreTitle: (time) => `Khôi phục phiên bản lúc ${time}?`,
    restorePublic: (link) =>
      `CV này đang công khai. Người mở ${link} sẽ thấy phiên bản này ngay `
      + 'khi bạn khôi phục.',
    restorePrivate: 'CV này chưa công khai, nên chỉ bạn thấy thay đổi.',
    restoreWhat: (time) =>
      'Nội dung, thông tin cá nhân, bố cục, thiết kế, tiêu đề và ngôn ngữ '
      + `CV trở về như lúc ${time}.`,
    restoreNewTitle: (title) => `Tiêu đề CV sẽ là “${title}”.`,
    restoreNewLanguage: (language) => `Ngôn ngữ CV sẽ là ${language}.`,
    restoreKept:
      'Giữ nguyên: ảnh và vùng cắt ảnh, địa chỉ CV, cài đặt xuất bản và '
      + 'chia sẻ, tiêu đề và biểu tượng của trang công khai, cùng các dự án '
      + 'trong mục Dự án.',
    restoreUndoNote:
      'Phiên bản hiện tại được lưu vào lịch sử trước, nên bạn có thể hoàn '
      + 'tác.',
    cancel: 'Hủy',
    restoring: 'Đang khôi phục…',
    restoreChanged:
      'CV vừa thay đổi ở nơi khác, và trình chỉnh sửa đã tải bản mới nhất. '
      + 'Chọn Khôi phục để thử lại; bản mới nhất sẽ được lưu vào lịch sử '
      + 'trước.',
    restoreInvalid:
      'Không thể khôi phục phiên bản này vì nó không đạt các kiểm tra hiện '
      + 'tại của CV. CV không thay đổi.',
    restoreNetwork:
      'Chưa xác nhận được việc khôi phục vì lỗi kết nối. Hãy thử lại; CV sẽ '
      + 'không bị khôi phục hai lần.',
    restoreBusy: 'Hệ thống đang bận. Hãy thử lại sau ít phút.',
    restoreFailed: 'Chưa khôi phục được. CV không thay đổi.',
    restoredPrivate: (time) =>
      `Đã khôi phục phiên bản lúc ${time}. Phiên bản trước đó đã được lưu `
      + 'vào lịch sử.',
    restoredPublic: (time, link) =>
      `Đã khôi phục phiên bản lúc ${time}, và ${link} đang hiển thị phiên `
      + 'bản này. Phiên bản trước đó đã được lưu vào lịch sử.',
    undo: 'Hoàn tác',
    deleteTitle: (time) => `Xóa phiên bản lúc ${time}?`,
    deleteBody:
      'Phiên bản này sẽ bị xóa khỏi lịch sử và không thể khôi phục. CV hiện '
      + 'tại không thay đổi.',
    deleteConfirm: 'Xóa phiên bản',
    deleting: 'Đang xóa…',
    clearing: 'Đang xóa…',
    deleteFailed: 'Chưa xóa được. Hãy thử lại.',
    deleted: (time) => `Đã xóa phiên bản lúc ${time}.`,
    clear: 'Xóa toàn bộ lịch sử',
    clearConfirm: 'Xóa lịch sử',
    clearTitle: 'Xóa toàn bộ lịch sử của CV này?',
    clearBody: (n) =>
      `Thao tác này xóa vĩnh viễn ${n} phiên bản khỏi lịch sử. Nội dung bạn `
      + 'đã xóa khỏi CV sẽ không còn được giữ ở đây. CV hiện tại không thay '
      + 'đổi.',
    clearFailed: 'Chưa xóa được lịch sử. Hãy thử lại.',
    cleared: 'Đã xóa toàn bộ lịch sử.',
  },
  en: {
    lead: 'Each version is the resume just before a round of edits.',
    count: (n) => `${n} / 50`,
    countName: (n) => `${n} of at most 50 versions`,
    listLabel: 'Resume versions',
    current: 'Current version',
    currentDetail: 'The resume as it is in the editor',
    causeEdit: 'Before your edits',
    causeAssistant: (name) => `Before edits by the assistant ${name}`,
    causeAssistantUnnamed: 'Before edits by an AI assistant',
    causeRestore: 'Before you restored a version',
    causeAccept: 'Before you accepted proposed changes',
    retention:
      'History keeps up to 50 versions for 30 days. When it is full, the '
      + 'oldest is removed automatically.',
    emptyTitle: 'No versions yet',
    emptyBody:
      'When you or an AI assistant start editing, the resume as it was '
      + 'before is kept here for 30 days.',
    loading: 'Loading history',
    loadFailed: 'Couldn\'t load history.',
    versionTitle: (time) => `Version from ${time}`,
    viewOnly: 'View only. Restoring keeps your current photo.',
    previewLabel: (time) => `Preview of the version from ${time}`,
    showing: (time) => `Showing the version from ${time}`,
    previewLoading: 'Loading version',
    previewFailed: 'Couldn\'t load this version.',
    versionGone:
      'This version is no longer in history. It may be over 30 days old or '
      + 'deleted.',
    backToEditing: 'Back to editing',
    restore: 'Restore',
    restoreBlocked: 'You can restore once every change is saved.',
    deleteVersionLabel: 'Delete this version',
    restoreTitle: (time) => `Restore the version from ${time}?`,
    restorePublic: (link) =>
      `This resume is public. Anyone who opens ${link} sees this version `
      + 'as soon as you restore it.',
    restorePrivate: 'This resume isn\'t public, so only you see the change.',
    restoreWhat: (time) =>
      'The text, personal details, layout, design, title and resume '
      + `language go back to how they were at ${time}.`,
    restoreNewTitle: (title) => `The resume title becomes “${title}”.`,
    restoreNewLanguage: (language) =>
      `The resume language becomes ${language}.`,
    restoreKept:
      'Kept as they are: the photo and its crop, the resume address, '
      + 'publish and sharing settings, the public page title and icon, and '
      + 'the projects in Projects.',
    restoreUndoNote:
      'The current version is saved to history first, so you can undo this.',
    cancel: 'Cancel',
    restoring: 'Restoring…',
    restoreChanged:
      'The resume just changed elsewhere, and the editor now has the latest '
      + 'version. Choose Restore to try again; the latest version is saved '
      + 'to history first.',
    restoreInvalid:
      'This version can\'t be restored because it doesn\'t pass the current '
      + 'resume checks. Nothing changed.',
    restoreNetwork:
      'We couldn\'t confirm the restore because of a connection problem. '
      + 'Try again; it won\'t be applied twice.',
    restoreBusy: 'The service is busy. Try again in a few minutes.',
    restoreFailed: 'Couldn\'t restore. Nothing changed.',
    restoredPrivate: (time) =>
      `Restored the version from ${time}. The version before it is saved `
      + 'in history.',
    restoredPublic: (time, link) =>
      `Restored the version from ${time}, and ${link} now shows it. The `
      + 'version before it is saved in history.',
    undo: 'Undo',
    deleteTitle: (time) => `Delete the version from ${time}?`,
    deleteBody:
      'This version is removed from history and can\'t be restored. Your '
      + 'resume as it is now doesn\'t change.',
    deleteConfirm: 'Delete version',
    deleting: 'Deleting…',
    clearing: 'Clearing…',
    deleteFailed: 'Couldn\'t delete. Try again.',
    deleted: (time) => `Deleted the version from ${time}.`,
    clear: 'Clear history',
    clearConfirm: 'Clear history',
    clearTitle: 'Clear this resume\'s history?',
    clearBody: (n) =>
      `This deletes ${n === 1 ? '1 version' : `${n} versions`} from history `
      + 'for good. Text you removed from the resume is no longer kept here. '
      + 'Your resume as it is now doesn\'t change.',
    clearFailed: 'Couldn\'t clear history. Try again.',
    cleared: 'History cleared.',
  },
};
