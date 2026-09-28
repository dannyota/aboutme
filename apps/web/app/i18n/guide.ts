// The MCP guide page (/guide/mcp) in both site languages. Copy follows
// docs/design/mcp-guide.md and docs/design/mcp-guide-copy.md, in page order.
// Claude's own UI names (button and menu labels) stay in English on both
// languages, matching what Claude shows (mcp-guide-copy.md, "Connect
// Claude").
import { siteOrigin } from './meta';
import type { WorkspaceCopy } from './workspace';

/** The one MCP server URL the page teaches, built from the site origin. */
export const mcpServerUrl = `${siteOrigin}/mcp`;

/**
 * The shell prompt marker a command block shows but never copies
 * (docs/design/mcp-guide.md, "Layout": the URL block carries no prompt).
 * Kept as data, not a template literal, like every other display string.
 */
export const shellPrompt = '$ ';

/** Claude Code command text, identical in both languages. */
export const addServerCommand
  = `claude mcp add --transport http --scope user aboutme ${mcpServerUrl}`;
export const loginCommand = 'claude mcp login aboutme';

/** Anthropic's own help page for adding a custom connector. */
export const claudeConnectorsHelpUrl = 'https://support.claude.com/en/'
  + 'articles/11175166-get-started-with-custom-connectors-using-remote-mcp';

export const settingsSessionsPath = '/app/settings/sessions';

/**
 * One run of inline text. A line mixing plain text with a UI name, inline
 * code, or a link is an array of these, rendered by GuideRichText.vue so no
 * display text sits in a template literal (docs/standards/engineering.md).
 */
export type GuideInline
  = { readonly kind: 'text'; readonly text: string }
    | { readonly kind: 'strong'; readonly text: string }
    | { readonly kind: 'code'; readonly text: string }
    | {
      readonly kind: 'link';
      readonly text: string;
      readonly href: string;
      readonly external: boolean;
    };

export type GuideLine = readonly GuideInline[];

function t(text: string): GuideInline {
  return { kind: 'text', text };
}
function b(text: string): GuideInline {
  return { kind: 'strong', text };
}
function c(text: string): GuideInline {
  return { kind: 'code', text };
}
function a(text: string, href: string, external = false): GuideInline {
  return { kind: 'link', text, href, external };
}
/** Wraps a plain string as a one-part line, for a uniform line type. */
function plain(text: string): GuideLine {
  return [t(text)];
}

export interface GuideCommandStep {
  readonly line: GuideLine;
  readonly command: string;
  /** Accessible name for the scrolling command block. */
  readonly scrollLabel: string;
}

export interface GuideCopy {
  readonly title: string;
  readonly description: string;
  readonly header: {
    readonly h1: string;
    readonly lead: string;
    readonly explainer: string;
    readonly urlLabel: string;
    readonly copyUrlLabel: string;
    readonly urlCopied: string;
    readonly copyFailed: string;
    readonly account: GuideLine;
  };
  readonly canCannot: {
    readonly heading: string;
    readonly canHeading: string;
    readonly can: readonly string[];
    readonly cannotHeading: string;
    readonly cannot: readonly string[];
    readonly scope: string;
  };
  readonly claude: {
    readonly heading: string;
    readonly intro: string;
    readonly webHeading: string;
    readonly webSteps: readonly GuideLine[];
    readonly planNote: GuideLine;
    readonly codeHeading: string;
    readonly addStep: GuideCommandStep;
    readonly loginStep: GuideCommandStep;
    readonly removeStep: GuideLine;
    readonly copyCommandLabel: string;
    readonly commandCopied: string;
    readonly commandCopyFailed: string;
  };
  readonly tryRequest: {
    readonly heading: string;
    readonly examples: readonly string[];
    readonly after: string;
  };
  readonly privacy: {
    readonly heading: string;
    readonly points: readonly GuideLine[];
  };
  readonly otherApps: {
    readonly heading: string;
    readonly requirements: GuideLine;
    readonly limits: GuideLine;
  };
  readonly troubleshooting: {
    readonly heading: string;
    readonly pairs: readonly {
      readonly question: string;
      readonly answer: GuideLine;
    }[];
  };
}

export const guideCopy: WorkspaceCopy<GuideCopy> = {
  vi: {
    title: 'Kết nối trợ lý AI qua MCP',
    description: 'Kết nối Claude hoặc trợ lý AI khác với aboutme.vn qua MCP '
      + 'để đọc và sửa CV. Bạn duyệt quyền, thu hồi bất cứ lúc nào và tự '
      + 'quyết định xuất bản.',
    header: {
      h1: 'Kết nối trợ lý AI với aboutme.vn',
      lead: 'Dùng Claude hoặc một trợ lý AI khác hỗ trợ MCP để đọc và chỉnh '
        + 'sửa CV của bạn. Bạn duyệt từng kết nối, còn việc xuất bản vẫn do '
        + 'bạn quyết định.',
      explainer: 'MCP (Model Context Protocol) là một chuẩn mở giúp trợ lý '
        + 'AI dùng công cụ của dịch vụ khác. aboutme.vn không chạy mô hình '
        + 'AI nào; trợ lý bạn chọn làm việc với CV qua đúng những bước kiểm '
        + 'tra mà trình chỉnh sửa dùng.',
      urlLabel: 'Địa chỉ máy chủ MCP',
      copyUrlLabel: 'Sao chép địa chỉ',
      urlCopied: 'Đã sao chép địa chỉ máy chủ MCP.',
      copyFailed: 'Không sao chép được. Hãy chọn địa chỉ và tự sao chép.',
      account: [
        t('Bạn cần một tài khoản aboutme.vn. '),
        a('Tạo tài khoản', '/register'),
        t(' hoặc '),
        a('đăng nhập', '/login'),
        t(' trước để kết nối nhanh hơn.'),
      ],
    },
    canCannot: {
      heading: 'Trợ lý AI làm được gì',
      canHeading: 'Được phép',
      can: [
        'Xem danh sách và đọc CV của bạn, kể cả ảnh chân dung.',
        'Tạo CV mới, trong giới hạn ba CV mỗi tài khoản.',
        'Sửa nội dung, các mục, bố cục, thông tin cá nhân và giao diện.',
        'Tải lên, cắt hoặc xóa ảnh chân dung.',
        'Xóa CV. Xóa một CV đã xuất bản cũng gỡ đường dẫn công khai của CV '
        + 'đó.',
      ],
      cannotHeading: 'Không được phép',
      cannot: [
        'Xuất bản, hủy xuất bản, hay đổi cài đặt PDF và lập chỉ mục. Bạn '
        + 'làm việc này trong trình chỉnh sửa.',
        'Đọc CV của người khác.',
        'Xem hay đổi mật khẩu, email, phiên đăng nhập hoặc cài đặt tài '
        + 'khoản.',
        'Xem lượt xem, xuất PDF hoặc nhập hồ sơ LinkedIn.',
        'Lưu thứ mà trình chỉnh sửa sẽ từ chối. Mọi thay đổi đều qua cùng '
        + 'các bước kiểm tra.',
      ],
      scope: 'Quyền áp dụng cho mọi CV trong tài khoản; không giới hạn được '
        + 'cho riêng một CV.',
    },
    claude: {
      heading: 'Kết nối Claude',
      intro: 'Claude hỗ trợ máy chủ MCP từ xa có đăng nhập. Tên nút và menu '
        + 'bên dưới được giữ nguyên như Claude hiển thị.',
      webHeading: 'Claude trên web, máy tính và điện thoại',
      webSteps: [
        [t('Trong Claude, mở '), b('Customize'), t(', rồi '), b('Connectors'),
          t('.')],
        [t('Chọn '), b('+'), t(', rồi '), b('Add custom connector'), t('.')],
        [t('Đặt tên '), c('aboutme.vn'), t(' và dán '), c(mcpServerUrl),
          t(' vào ô URL. Để trống '), b('Advanced settings'), t('.')],
        [t('Chọn '), b('Add'), t('. Nếu Claude hiện '), b('Connect'),
          t(', hãy chọn nút đó.')],
        [t('Trang aboutme.vn mở ra. Đăng nhập nếu được hỏi, kiểm tra yêu '
          + 'cầu ghi tên Claude và quay lại claude.ai, rồi chọn '),
        b('Cho phép'), t('.')],
        [t('Trong một cuộc trò chuyện, chọn '), b('+'), t(', rồi '),
          b('Connectors'), t(', và bật aboutme.vn.')],
      ],
      planNote: [
        t('Gói Free cho phép một connector tùy chỉnh. Với gói Team hoặc '
          + 'Enterprise, chủ sở hữu tổ chức thêm connector trong '),
        b('Organization settings'), t(' > '), b('Connectors'),
        t(', rồi mỗi thành viên chọn '), b('Connect'), t('. '),
        a('Trang trợ giúp của Claude', claudeConnectorsHelpUrl, true),
      ],
      codeHeading: 'Claude Code',
      addStep: {
        line: plain('Thêm máy chủ cho mọi dự án của bạn:'),
        command: addServerCommand,
        scrollLabel: 'Lệnh thêm máy chủ aboutme.vn',
      },
      loginStep: {
        line: [t('Đăng nhập. Trình duyệt mở aboutme.vn; chọn '),
          b('Cho phép'), t('. Trong Claude Code, bạn cũng có thể gõ '),
          c('/mcp'), t(' rồi chọn aboutme.')],
        command: loginCommand,
        scrollLabel: 'Lệnh đăng nhập máy chủ aboutme',
      },
      removeStep: [t('Để gỡ, chạy '), c('claude mcp remove aboutme'),
        t(', rồi thu hồi quyền trong Cài đặt của aboutme.vn.')],
      copyCommandLabel: 'Sao chép lệnh',
      commandCopied: 'Đã sao chép lệnh.',
      commandCopyFailed: 'Không sao chép được. Hãy chọn lệnh và tự sao '
        + 'chép.',
    },
    tryRequest: {
      heading: 'Thử một yêu cầu',
      examples: [
        'Đọc CV của tôi và đề xuất một phần tóm tắt ngắn gọn hơn.',
        'Tạo một bản CV tiếng Việt từ CV tiếng Anh của tôi. Giữ nguyên tên '
        + 'công ty, ngày tháng và số liệu.',
        'Viết lại các gạch đầu dòng ở công việc gần nhất để nêu kết quả '
        + 'trước, nhưng không thêm điều tôi chưa làm.',
      ],
      after: 'Mở CV trong trình chỉnh sửa để xem lại thay đổi, rồi tự xuất '
        + 'bản khi bạn sẵn sàng.',
    },
    privacy: {
      heading: 'Quyền riêng tư và quyền kiểm soát',
      points: [
        plain('Bạn duyệt từng kết nối trên aboutme.vn. Trang cấp quyền ghi '
          + 'tên ứng dụng, nơi bạn sẽ quay lại, và hai quyền: Đọc CV và '
          + 'Chỉnh sửa CV.'),
        [t('Chỉ cho phép yêu cầu do chính bạn vừa bắt đầu. Nếu trang cấp '
          + 'quyền mở ra khi bạn không kết nối gì, hãy chọn '),
        b('Từ chối'), t('.')],
        plain('Trợ lý không bao giờ nhận mật khẩu hay phiên đăng nhập của '
          + 'bạn, chỉ nhận một mã truy cập riêng mà bạn thu hồi được.'),
        [t('Nội dung trợ lý đọc được sẽ đến dịch vụ AI bạn chọn và theo '
          + 'chính sách của dịch vụ đó. Xem '),
        a('Chính sách quyền riêng tư', '/privacy'), t('.')],
        [t('Thu hồi bất cứ lúc nào trong '),
          a('Cài đặt', settingsSessionsPath),
          t(' > Tác nhân đã kết nối. Yêu cầu tiếp theo của trợ lý sẽ bị từ '
            + 'chối; các thay đổi đã lưu vẫn còn.')],
      ],
    },
    otherApps: {
      heading: 'Ứng dụng khác',
      requirements: [
        t('Ứng dụng hỗ trợ máy chủ MCP từ xa (Streamable HTTP) với đăng '
          + 'nhập OAuth và tự đăng ký ứng dụng có thể dùng cùng địa chỉ '),
        c(mcpServerUrl), t('.'),
      ],
      limits: plain('Ứng dụng chỉ chạy máy chủ MCP cục bộ, chỉ nhận khóa '
        + 'API, hoặc quay về một địa chỉ riêng của ứng dụng thay vì https '
        + 'hay localhost thì chưa kết nối được.'),
    },
    troubleshooting: {
      heading: 'Khắc phục sự cố',
      pairs: [
        {
          question: 'Claude báo không kết nối được.',
          answer: [t('Kiểm tra địa chỉ đúng là '), c(mcpServerUrl),
            t(', không có dấu cách hay dấu gạch chéo ở cuối. Gỡ connector '
              + 'rồi thêm lại.')],
        },
        {
          question: 'Trang cấp quyền báo yêu cầu không hợp lệ.',
          answer: plain('Yêu cầu đã hết hạn hoặc đã được dùng. Bắt đầu kết '
            + 'nối lại từ trợ lý.'),
        },
        {
          question: 'Trang báo bạn đã có 10 trợ lý được kết nối.',
          answer: [t('Thu hồi một trợ lý cũ trong '),
            a('Cài đặt', settingsSessionsPath), t(', rồi kết nối lại.')],
        },
        {
          question: 'Báo quá nhiều lần thử.',
          answer: plain('aboutme.vn giới hạn số lần ứng dụng đăng ký và '
            + 'đăng nhập. Đợi đến một giờ rồi thử lại.'),
        },
        {
          question: 'Trợ lý không tạo được CV mới.',
          answer: plain('Mỗi tài khoản có tối đa ba CV. Xóa một CV hoặc để '
            + 'trợ lý sửa CV có sẵn.'),
        },
        {
          question: 'Trợ lý báo CV đã thay đổi hoặc không xuất bản được.',
          answer: plain('CV đã được sửa ở nơi khác, ví dụ trong trình '
            + 'chỉnh sửa: bảo trợ lý đọc lại rồi thử lại. Việc xuất bản thì '
            + 'luôn do bạn làm trong trình chỉnh sửa.'),
        },
      ],
    },
  },
  en: {
    title: 'Connect your AI assistant with MCP',
    description: 'Connect Claude or another AI assistant to aboutme.vn '
      + 'through MCP to read and edit your resumes. You approve access, '
      + 'revoke it any time, and decide what gets published.',
    header: {
      h1: 'Connect your AI assistant to aboutme.vn',
      lead: 'Use Claude or another MCP-capable AI assistant to read and '
        + 'edit your resumes. You approve each connection, and publishing '
        + 'stays your decision.',
      explainer: 'MCP (Model Context Protocol) is an open standard that '
        + 'lets an AI assistant use another service\'s tools. aboutme.vn '
        + 'runs no AI model; the assistant you choose works on your '
        + 'resumes through the same checks as the editor.',
      urlLabel: 'MCP server URL',
      copyUrlLabel: 'Copy URL',
      urlCopied: 'Copied the MCP server URL.',
      copyFailed: 'Could not copy. Select the URL and copy it yourself.',
      account: [
        t('You need an aboutme.vn account. '),
        a('Create an account', '/register'),
        t(' or '),
        a('sign in', '/login'),
        t(' first to connect faster.'),
      ],
    },
    canCannot: {
      heading: 'What your assistant can do',
      canHeading: 'Can',
      can: [
        'List and read your resumes, including the photo.',
        'Create a resume, within the limit of three per account.',
        'Edit content, sections, layout, personal details, and styling.',
        'Upload, crop, or remove the photo.',
        'Delete a resume. Deleting a published resume also takes its '
        + 'public link down.',
      ],
      cannotHeading: 'Cannot',
      cannot: [
        'Publish, unpublish, or change PDF and indexing settings. You do '
        + 'that in the editor.',
        'Read anyone else\'s resumes.',
        'See or change your password, email, sessions, or account '
        + 'settings.',
        'See view counts, export a PDF, or import from LinkedIn.',
        'Save anything the editor would reject. Every change passes the '
        + 'same checks.',
      ],
      scope: 'Access covers every resume in your account; you cannot '
        + 'limit it to one resume.',
    },
    claude: {
      heading: 'Connect Claude',
      intro: 'Claude supports remote MCP servers with sign-in. Button and '
        + 'menu names below are as Claude shows them.',
      webHeading: 'Claude on the web, desktop, and mobile',
      webSteps: [
        [t('In Claude, open '), b('Customize'), t(', then '),
          b('Connectors'), t('.')],
        [t('Choose '), b('+'), t(', then '), b('Add custom connector'),
          t('.')],
        [t('Name it '), c('aboutme.vn'), t(' and paste '), c(mcpServerUrl),
          t(' as the URL. Leave '), b('Advanced settings'), t(' empty.')],
        [t('Choose '), b('Add'), t('. If Claude shows '), b('Connect'),
          t(', choose it.')],
        [t('An aboutme.vn page opens. Sign in if asked, check that the '
          + 'request names Claude and returns to claude.ai, then choose '),
        b('Approve'), t('.')],
        [t('In a chat, choose '), b('+'), t(', then '), b('Connectors'),
          t(', and turn on aboutme.vn.')],
      ],
      planNote: [
        t('The Free plan allows one custom connector. On Team and '
          + 'Enterprise plans, an organization owner adds it in '),
        b('Organization settings'), t(' > '), b('Connectors'),
        t(', then each member chooses '), b('Connect'), t('. '),
        a('Claude\'s help page', claudeConnectorsHelpUrl, true),
      ],
      codeHeading: 'Claude Code',
      addStep: {
        line: plain('Add the server for all your projects:'),
        command: addServerCommand,
        scrollLabel: 'Command to add the aboutme.vn server',
      },
      loginStep: {
        line: [t('Sign in. Your browser opens aboutme.vn; choose '),
          b('Approve'), t('. Inside Claude Code you can also run '),
          c('/mcp'), t(' and pick aboutme.')],
        command: loginCommand,
        scrollLabel: 'Command to sign in to aboutme',
      },
      removeStep: [t('To remove it, run '), c('claude mcp remove aboutme'),
        t(', then revoke access in aboutme.vn Settings.')],
      copyCommandLabel: 'Copy command',
      commandCopied: 'Copied the command.',
      commandCopyFailed: 'Could not copy. Select the command and copy it '
        + 'yourself.',
    },
    tryRequest: {
      heading: 'Try a request',
      examples: [
        'Read my resume and suggest a shorter summary.',
        'Make a Vietnamese copy of my English resume. Keep company '
        + 'names, dates, and numbers exactly.',
        'Rewrite the bullets in my latest job to lead with results, '
        + 'without adding anything I did not do.',
      ],
      after: 'Open the resume in the editor to review the changes, then '
        + 'publish it yourself when you are ready.',
    },
    privacy: {
      heading: 'Privacy and control',
      points: [
        plain('You approve each connection on aboutme.vn. The approval '
          + 'page names the app, where you return, and two permissions: '
          + 'Read resumes and Write resumes.'),
        [t('Approve only a request you just started. If an approval page '
          + 'opens when you did not connect anything, choose '),
        b('Deny'), t('.')],
        plain('The assistant never gets your password or sign-in session, '
          + 'only its own access token, which you can revoke.'),
        [t('Content the assistant reads goes to the AI service you '
          + 'chose, under that service\'s policy. See the '),
        a('Privacy Policy', '/privacy'), t('.')],
        [t('Revoke any time in '), a('Settings', settingsSessionsPath),
          t(' > Connected agents. The assistant\'s next request is '
            + 'refused; changes it already saved stay.')],
      ],
    },
    otherApps: {
      heading: 'Other apps',
      requirements: [
        t('An app that supports remote MCP servers (Streamable HTTP) '
          + 'with OAuth sign-in and automatic client registration can use '
          + 'the same URL, '),
        c(mcpServerUrl), t('.'),
      ],
      limits: plain('Apps that only run local MCP servers, only accept '
        + 'API keys, or return to an app-specific address instead of '
        + 'https or localhost cannot connect yet.'),
    },
    troubleshooting: {
      heading: 'Troubleshooting',
      pairs: [
        {
          question: 'Claude says it could not connect.',
          answer: [t('Check that the URL is exactly '), c(mcpServerUrl),
            t(', with no space or trailing slash. Remove the connector '
              + 'and add it again.')],
        },
        {
          question: 'The approval page says the request is invalid.',
          answer: plain('The request expired or was already used. Start '
            + 'the connection again from your assistant.'),
        },
        {
          question: 'The page says you already have 10 connected agents.',
          answer: [t('Revoke an old one in '),
            a('Settings', settingsSessionsPath), t(', then connect '
              + 'again.')],
        },
        {
          question: 'It says there were too many attempts.',
          answer: plain('aboutme.vn limits how often apps register and '
            + 'sign in. Wait up to an hour and try again.'),
        },
        {
          question: 'The assistant cannot create a resume.',
          answer: plain('Each account holds at most three resumes. '
            + 'Delete one, or have the assistant edit an existing one.'),
        },
        {
          question: 'The assistant says the resume changed, or it cannot '
            + 'publish.',
          answer: plain('The resume was edited somewhere else, such as '
            + 'the editor: ask the assistant to read it again and retry. '
            + 'Publishing is always yours to do in the editor.'),
        },
      ],
    },
  },
};
