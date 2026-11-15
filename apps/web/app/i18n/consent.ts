import type { OAuthConsentScope } from '../composables/useOAuthConsent';
import type { WorkspaceCopy } from './workspace';

type ConsentCopy = {
  readonly title: string;
  readonly allowClient: (clientName: string) => string;
  readonly allowClientProjects: (clientName: string) => string;
  readonly clientRequest: string;
  readonly clientRequestProjects: string;
  readonly hostRequestAfter: string;
  readonly hostRequestAfterProjects: string;
  readonly projectsPublicAtOnce: string;
  readonly selfNamedBefore: string;
  readonly selfNamedAfter: string;
  readonly loopbackWarning: string;
  readonly loading: string;
  readonly invalid: string;
  readonly unavailable: string;
  readonly reauthRequired: string;
  readonly restartRequired: string;
  readonly signInAgain: string;
  readonly returnToHost: (host: string) => string;
  readonly returnToLoopback: (host: string) => string;
  readonly agentLimit: {
    readonly before: string;
    readonly settingsLabel: string;
    readonly after: string;
  };
  readonly requestedPermissions: string;
  readonly scopes: Record<
    OAuthConsentScope,
    { readonly name: string; readonly description: string }
  >;
  readonly approve: string;
  readonly working: string;
  readonly deny: string;
};

export const consentCopy: WorkspaceCopy<ConsentCopy> = {
  vi: {
    title: 'Cấp quyền truy cập',
    allowClient: (clientName) => `Cho phép ${clientName} chỉnh sửa CV của bạn?`,
    allowClientProjects: (clientName) =>
      `Cho phép ${clientName} chỉnh sửa CV và dự án của bạn?`,
    clientRequest: 'đang yêu cầu quyền truy cập CV của bạn.',
    clientRequestProjects: 'đang yêu cầu quyền truy cập CV và dự án của bạn.',
    hostRequestAfter: ' muốn truy cập CV của bạn.',
    hostRequestAfterProjects: ' muốn truy cập CV và dự án của bạn.',
    projectsPublicAtOnce:
      'Thay đổi trên một dự án đang hiện trên CV đã xuất bản '
      + 'sẽ công khai ngay.',
    selfNamedBefore: 'Ứng dụng tự gọi là “',
    selfNamedAfter: '”.',
    loopbackWarning:
      'Chỉ cho phép nếu bạn vừa bắt đầu đăng nhập '
      + 'từ một ứng dụng trên máy tính này.',
    loading: 'Đang tải yêu cầu cấp quyền…',
    invalid: 'Yêu cầu cấp quyền này đã hết hạn hoặc không hợp lệ. '
      + 'Hãy bắt đầu lại từ ứng dụng của bạn.',
    unavailable: 'Không thể tải yêu cầu cấp quyền. Hãy thử lại.',
    reauthRequired: 'Để cấp quyền truy cập, bạn cần đăng nhập lại. '
      + 'Sau đó, bạn sẽ quay lại yêu cầu này.',
    restartRequired:
      'Hãy đăng nhập lại, sau đó bắt đầu lại yêu cầu cấp quyền '
      + 'từ ứng dụng của bạn.',
    signInAgain: 'Đăng nhập lại',
    returnToHost: (host) => `Sau khi cho phép, bạn sẽ quay lại ${host}.`,
    returnToLoopback: (host) =>
      'Sau khi cho phép, bạn quay lại một ứng dụng trên máy tính này '
      + `(${host}).`,
    agentLimit: {
      before: 'Bạn đã có 10 trợ lý được kết nối. Thu hồi một trợ lý trong ',
      settingsLabel: 'Cài đặt',
      after: ' rồi thử lại.',
    },
    requestedPermissions: 'Quyền được yêu cầu',
    scopes: {
      'resumes:read': {
        name: 'Đọc CV',
        description: 'Xem CV của bạn.',
      },
      'resumes:write': {
        name: 'Chỉnh sửa CV',
        description: 'Tạo và chỉnh sửa CV của bạn.',
      },
      'projects:read': {
        name: 'Đọc dự án',
        description: 'Xem các dự án trong thư viện của bạn.',
      },
      'projects:write': {
        name: 'Chỉnh sửa dự án',
        description: 'Tạo, sửa và xóa dự án trong thư viện của bạn.',
      },
    },
    approve: 'Cho phép',
    working: 'Đang xử lý…',
    deny: 'Từ chối',
  },
  en: {
    title: 'Allow access',
    allowClient: (clientName) => `Allow ${clientName} to edit your resumes?`,
    allowClientProjects: (clientName) =>
      `Allow ${clientName} to edit your resumes and projects?`,
    clientRequest: 'is requesting access to your resumes.',
    clientRequestProjects: 'is requesting access to your resumes and projects.',
    hostRequestAfter: ' wants access to your resumes.',
    hostRequestAfterProjects: ' wants access to your resumes and projects.',
    projectsPublicAtOnce:
      'A change to a project shown on a published resume is public at once.',
    selfNamedBefore: 'The app calls itself “',
    selfNamedAfter: '”.',
    loopbackWarning:
      'Approve only if you just started this sign-in '
      + 'from an app on this computer.',
    loading: 'Loading authorization…',
    invalid: 'This authorization request has expired or is invalid. '
      + 'Start again from your app.',
    unavailable: 'Unable to load authorization. Please try again.',
    reauthRequired: 'Sign in again to allow access. '
      + 'You will return to this request afterward.',
    restartRequired:
      'Sign in again, then start authorization again from your app.',
    signInAgain: 'Sign in again',
    returnToHost: (host) => `After you approve, you return to ${host}.`,
    returnToLoopback: (host) =>
      `After you approve, you return to an app on this computer (${host}).`,
    agentLimit: {
      before: 'You already have 10 connected agents. Revoke one in ',
      settingsLabel: 'Settings',
      after: ', then try again.',
    },
    requestedPermissions: 'Requested permissions',
    scopes: {
      'resumes:read': {
        name: 'Read resumes',
        description: 'View your resumes.',
      },
      'resumes:write': {
        name: 'Write resumes',
        description: 'Create and edit your resumes.',
      },
      'projects:read': {
        name: 'Read projects',
        description: 'View the projects in your library.',
      },
      'projects:write': {
        name: 'Write projects',
        description: 'Create, edit, and delete projects in your library.',
      },
    },
    approve: 'Approve',
    working: 'Working…',
    deny: 'Deny',
  },
};
