import { expect, type Page } from '@playwright/test';

const COPY = {
  vi: {
    intro: 'Claude hỗ trợ máy chủ MCP từ xa có đăng nhập. Tên nút và menu '
      + 'bên dưới được giữ nguyên như Claude hiển thị.',
    plan: 'Gói Free cho phép một connector tùy chỉnh. Với gói Team hoặc '
      + 'Enterprise, chủ sở hữu tổ chức thêm connector trong Organization '
      + 'settings > Connectors, rồi mỗi thành viên chọn Connect. '
      + 'Trang trợ giúp của Claude',
    other: [
      'Để dùng https://aboutme.vn/mcp, ứng dụng cần hỗ trợ MCP từ xa '
      + '(Streamable HTTP) và đăng nhập OAuth. Ứng dụng phải tự đăng ký '
      + 'hoặc dùng tài liệu mô tả ứng dụng (client ID metadata document) '
      + 'qua HTTPS.',
      'Ứng dụng chỉ chạy máy chủ MCP cục bộ, chỉ nhận khóa API, hoặc quay '
      + 'về một địa chỉ riêng của ứng dụng thay vì https hay localhost '
      + 'thì chưa kết nối được.',
    ],
    after: 'Mở CV trong trình chỉnh sửa để xem lại thay đổi, rồi tự xuất '
      + 'bản khi bạn sẵn sàng.',
    scope: 'Quyền áp dụng cho mọi CV trong tài khoản; không giới hạn được '
      + 'cho riêng một CV.',
    explainer: 'MCP (Model Context Protocol) là một chuẩn mở giúp trợ lý '
      + 'AI dùng công cụ của dịch vụ khác. aboutme.vn không chạy mô hình '
      + 'AI nào; trợ lý bạn chọn làm việc với CV qua đúng những bước kiểm '
      + 'tra mà trình chỉnh sửa dùng.',
    account: 'Bạn cần một tài khoản aboutme.vn. Tạo tài khoản hoặc đăng nhập '
      + 'trước để kết nối nhanh hơn.',
  },
  en: {
    intro: 'Claude supports remote MCP servers with sign-in. Button and '
      + 'menu names below are as Claude shows them.',
    plan: 'The Free plan allows one custom connector. On Team and '
      + 'Enterprise plans, an organization owner adds it in Organization '
      + 'settings > Connectors, then each member chooses Connect. '
      + 'Claude\'s help page',
    other: [
      'To use https://aboutme.vn/mcp, an app needs remote MCP support '
      + '(Streamable HTTP) and OAuth sign-in. It must support automatic '
      + 'client registration or use a client ID metadata document served '
      + 'over HTTPS.',
      'Apps that only run local MCP servers, only accept API keys, or '
      + 'return to an app-specific address instead of https or localhost '
      + 'cannot connect yet.',
    ],
    after: 'Open the resume in the editor to review the changes, then '
      + 'publish it yourself when you are ready.',
    scope: 'Access covers every resume in your account; you cannot limit '
      + 'it to one resume.',
    explainer: 'MCP (Model Context Protocol) is an open standard that lets '
      + 'an AI assistant use another service\'s tools. aboutme.vn runs no '
      + 'AI model; the assistant you choose works on your resumes through '
      + 'the same checks as the editor.',
    account: 'You need an aboutme.vn account. Create an account or sign in '
      + 'first to connect faster.',
  },
} as const;

export async function expectGuideProse(
  page: Page, locale: 'vi' | 'en',
): Promise<void> {
  const copy = COPY[locale];
  for (const [selector, text] of [
    ['[data-testid="guide-claude"] > p:first-of-type', copy.intro],
    ['[data-testid="guide-plan-note"]', copy.plan],
    ['[data-testid="guide-try-request"] > p', copy.after],
    ['[data-testid="guide-can-cannot"] > p', copy.scope],
    ['[data-testid="guide-header"] > p:nth-of-type(2)', copy.explainer],
    ['[data-testid="guide-header"] > p:last-of-type', copy.account],
  ] as const) {
    await expect(page.locator(selector), 'Guide explanatory copy.')
      .toHaveText(text);
  }
  await expect(page.getByTestId('guide-other-apps').locator('p'),
    'Guide other apps copy.').toHaveText([...copy.other]);
}
