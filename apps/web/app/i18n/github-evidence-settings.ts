import type { WorkspaceCopy } from './workspace';

export type GitHubEvidenceSettingsCopy = {
  readonly title: string;
  readonly privacy: string;
  readonly loading: string;
  readonly loadFailed: string;
  readonly retry: string;
  readonly notConnected: string;
  readonly connectedAs: string;
  readonly connectedOn: string;
  readonly sessionOpenUntil: string;
  readonly noSession: string;
  readonly connect: string;
  readonly continue: string;
  readonly done: string;
  readonly disconnect: string;
  readonly disconnectAsk: string;
  readonly disconnectKeep: string;
  readonly disconnectRemove: string;
  readonly connectedNotice: string;
  readonly failedNotice: string;
  readonly differentAccountNotice: string;
  readonly errors: {
    readonly unavailable: string;
    readonly sessionEnded: string;
    readonly upstream: string;
    readonly rateLimited: string;
    readonly csrfRejected: string;
    readonly connectionChanged: string;
    readonly failed: string;
  };
};

export const githubEvidenceSettingsCopy: WorkspaceCopy<
  GitHubEvidenceSettingsCopy
> = {
  vi: {
    title: 'Bằng chứng GitHub',
    privacy:
      'Khi kết nối, aboutme không xin GitHub quyền nào ngoài thông tin công '
      + 'khai của bạn. aboutme không bao giờ lưu token GitHub của bạn.',
    loading: 'Đang tải bằng chứng GitHub…',
    loadFailed: 'Không thể tải bằng chứng GitHub. Hãy thử lại.',
    retry: 'Thử lại',
    notConnected: 'Chưa kết nối tài khoản GitHub nào.',
    connectedAs: 'Đã kết nối tài khoản',
    connectedOn: 'Kết nối ngày',
    sessionOpenUntil: 'Phiên GitHub mở đến',
    noSession: 'Không có phiên GitHub nào đang mở.',
    connect: 'Kết nối GitHub',
    continue: 'Tiếp tục với GitHub',
    done: 'Xong',
    disconnect: 'Ngắt kết nối',
    disconnectAsk: 'Giữ các mục đã gắn dưới dạng đường dẫn thường?',
    disconnectKeep: 'Giữ đường dẫn',
    disconnectRemove: 'Xóa khỏi mọi dự án',
    connectedNotice: 'Đã kết nối GitHub.',
    failedNotice: 'Chưa kết nối được GitHub. Hãy thử lại.',
    differentAccountNotice:
      'Tài khoản GitHub này khác tài khoản đã kết nối. '
      + 'Hãy ngắt kết nối trước.',
    errors: {
      unavailable: 'Hiện chưa dùng được bằng chứng GitHub. Hãy thử lại sau.',
      sessionEnded: 'Phiên GitHub đã kết thúc trước đó.',
      upstream: 'GitHub hiện không khả dụng. Hãy thử lại sau.',
      rateLimited: 'Có quá nhiều lần thử. Hãy thử lại sau.',
      csrfRejected:
        'Không thể xác nhận yêu cầu này. Hãy tải lại trang rồi thử lại.',
      connectionChanged:
        'Kết nối GitHub vừa thay đổi. Trạng thái đã được tải lại; hãy thử lại.',
      failed: 'Đã xảy ra lỗi. Hãy thử lại.',
    },
  },
  en: {
    title: 'GitHub evidence',
    privacy:
      'Connecting asks GitHub for no access beyond your public information. '
      + 'aboutme never stores your GitHub token.',
    loading: 'Loading GitHub evidence…',
    loadFailed: 'GitHub evidence could not load. Try again.',
    retry: 'Retry',
    notConnected: 'No GitHub account connected.',
    connectedAs: 'Connected as',
    connectedOn: 'Connected on',
    sessionOpenUntil: 'GitHub session open until',
    noSession: 'No GitHub session is open.',
    connect: 'Connect GitHub',
    continue: 'Continue with GitHub',
    done: 'Done',
    disconnect: 'Disconnect',
    disconnectAsk: 'Keep the attached items as plain links?',
    disconnectKeep: 'Keep links',
    disconnectRemove: 'Remove from every project',
    connectedNotice: 'GitHub is connected.',
    failedNotice: 'GitHub was not connected. Try again.',
    differentAccountNotice:
      'This GitHub account differs from the connected one. Disconnect first.',
    errors: {
      unavailable:
        'GitHub evidence is not available right now. Try again later.',
      sessionEnded: 'The GitHub session had already ended.',
      upstream: 'GitHub is not available right now. Try again later.',
      rateLimited: 'Too many attempts. Try again later.',
      csrfRejected:
        'We could not confirm this request. Refresh the page and try again.',
      connectionChanged:
        'The GitHub connection just changed. The status has been reloaded; '
        + 'try again.',
      failed: 'Something went wrong. Please try again.',
    },
  },
};
