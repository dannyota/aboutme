import type { OAuthConsentScope } from '../composables/useOAuthConsent';
import type { WorkspaceCopy } from './workspace';

// Design rules cited below are in docs/design/mcp-client-compatibility.md.
type ConsentCopy = {
  readonly title: string;
  readonly allowClient: (clientName: string) => string;
  readonly clientRequest: string;
  readonly loading: string;
  readonly invalid: string;
  readonly unavailable: string;
  /** Where approval returns for an `https` redirect (design rule 6). */
  readonly returnToHost: (host: string) => string;
  /** Where approval returns for a loopback redirect (design rule 6). */
  readonly returnToLoopback: string;
  /** The ten-grant limit banner around the Settings link (design rule 7). */
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
    clientRequest: 'đang yêu cầu quyền truy cập CV của bạn.',
    loading: 'Đang tải yêu cầu cấp quyền…',
    invalid: 'Yêu cầu cấp quyền này không hợp lệ.',
    unavailable: 'Không thể tải yêu cầu cấp quyền. Hãy thử lại.',
    returnToHost: (host) => `Sau khi cho phép, bạn sẽ quay lại ${host}.`,
    returnToLoopback:
      'Sau khi cho phép, bạn quay lại một ứng dụng trên máy tính này.',
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
    },
    approve: 'Cho phép',
    working: 'Đang xử lý…',
    deny: 'Từ chối',
  },
  en: {
    title: 'Allow access',
    allowClient: (clientName) => `Allow ${clientName} to edit your resumes?`,
    clientRequest: 'is requesting access to your resumes.',
    loading: 'Loading authorization…',
    invalid: 'This authorization request is invalid.',
    unavailable: 'Unable to load authorization. Please try again.',
    returnToHost: (host) => `After you approve, you return to ${host}.`,
    returnToLoopback:
      'After you approve, you return to an app on this computer.',
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
    },
    approve: 'Approve',
    working: 'Working…',
    deny: 'Deny',
  },
};
