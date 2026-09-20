import type { AgentGrantScope } from '../composables/agentGrants';
import type { WorkspaceCopy } from './workspace';

type AgentSettingsCopy = {
  readonly title: string;
  readonly localeLabel: string;
  readonly loading: string;
  readonly unavailable: string;
  readonly retry: string;
  readonly emptyTitle: string;
  readonly emptyDescription: string;
  readonly scopes: Record<AgentGrantScope, string>;
  readonly created: string;
  readonly lastUsed: string;
  readonly neverUsed: string;
  readonly revoke: string;
  readonly revokeTitle: string;
  readonly revokeDescription: string;
  readonly revokeConfirm: string;
  readonly cancel: string;
};

export const agentSettingsCopy: WorkspaceCopy<AgentSettingsCopy> = {
  vi: {
    title: 'Tác nhân đã kết nối',
    localeLabel: 'Ngôn ngữ',
    loading: 'Đang tải tác nhân đã kết nối…',
    unavailable: 'Không thể tải tác nhân đã kết nối. Hãy thử lại.',
    retry: 'Thử lại',
    emptyTitle: 'Chưa có tác nhân nào được kết nối.',
    emptyDescription: 'Tác nhân kết nối qua MCP sau khi bạn cấp quyền.',
    scopes: {
      'resumes:read': 'Đọc CV',
      'resumes:write': 'Chỉnh sửa CV',
    },
    created: 'Đã tạo ngày',
    lastUsed: 'Dùng lần cuối vào',
    neverUsed: 'Chưa từng dùng',
    revoke: 'Thu hồi',
    revokeTitle: 'Thu hồi quyền truy cập',
    revokeDescription: 'Thu hồi quyền truy cập của tác nhân đã kết nối này?',
    revokeConfirm: 'Thu hồi quyền truy cập',
    cancel: 'Hủy',
  },
  en: {
    title: 'Connected agents',
    localeLabel: 'Language',
    loading: 'Loading connected agents…',
    unavailable: 'Connected agents are unavailable. Try again.',
    retry: 'Retry',
    emptyTitle: 'No connected agents.',
    emptyDescription: 'Agents connect through MCP after you approve access.',
    scopes: {
      'resumes:read': 'Read resumes',
      'resumes:write': 'Write resumes',
    },
    created: 'Created',
    lastUsed: 'Last used',
    neverUsed: 'Last used Never',
    revoke: 'Revoke',
    revokeTitle: 'Revoke access',
    revokeDescription: 'Revoke this connected agent\'s access?',
    revokeConfirm: 'Revoke access',
    cancel: 'Cancel',
  },
};
