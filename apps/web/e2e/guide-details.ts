import { expect, type Page } from '@playwright/test';

import { expectStyleRules, type StyleRule } from './screen-expectations';
import { expectSvg, lucideExpectation, visiblePaint } from './screen-paint';
import { expectGuideProse } from './guide-prose';

// Approved instruction copy: docs/design/mcp-guide-copy.md.
export const GUIDE_DETAILS = {
  vi: {
    web: [
      'Trong Claude, mở Customize, rồi Connectors.',
      'Chọn +, rồi Add custom connector.',
      'Đặt tên aboutme.vn và dán https://aboutme.vn/mcp vào ô URL. '
      + 'Để trống Advanced settings.',
      'Chọn Add. Nếu Claude hiện Connect, hãy chọn nút đó.',
      'Trang aboutme.vn mở ra. Đăng nhập nếu được hỏi, kiểm tra yêu cầu '
      + 'ghi tên Claude và quay lại claude.ai, rồi chọn Cho phép.',
      'Trong một cuộc trò chuyện, chọn +, rồi Connectors, và bật aboutme.vn.',
    ],
    code: [
      'Thêm máy chủ cho mọi dự án của bạn:',
      'Đăng nhập. Trên trang aboutme.vn, kiểm tra bên yêu cầu truy cập là '
      + 'claude.ai và nơi quay lại là ứng dụng trên máy tính này '
      + '(localhost), rồi chọn Cho phép. Bạn cần cho phép mỗi lần '
      + 'đăng nhập lại.',
      'Để gỡ, chạy claude mcp remove aboutme, rồi vào Cài đặt > Tác nhân '
      + 'đã kết nối trên aboutme.vn để thu hồi quyền của Claude Code.',
    ],
    trouble: [
      'Claude báo không kết nối được.',
      'Kiểm tra địa chỉ đúng là https://aboutme.vn/mcp, không có dấu cách '
      + 'hay dấu gạch chéo ở cuối. Gỡ connector rồi thêm lại.',
      'Trang cấp quyền báo yêu cầu không hợp lệ.',
      'Yêu cầu đã hết hạn hoặc đã được dùng. Bắt đầu kết nối lại từ trợ lý.',
      'Trang báo bạn đã có 10 trợ lý được kết nối.',
      'Thu hồi một trợ lý cũ trong Cài đặt, rồi kết nối lại.',
      'Báo quá nhiều lần thử.',
      'aboutme.vn giới hạn số lần ứng dụng đăng ký và đăng nhập. '
      + 'Đợi đến một giờ rồi thử lại.',
      'Trợ lý không tạo được CV mới.',
      'Mỗi tài khoản có tối đa ba CV. Xóa một CV hoặc để trợ lý sửa CV có sẵn.',
      'Trợ lý báo CV đã thay đổi hoặc không xuất bản được.',
      'CV đã được sửa ở nơi khác, ví dụ trong trình chỉnh sửa: bảo trợ lý '
      + 'đọc lại rồi thử lại. Việc xuất bản thì luôn do bạn làm trong '
      + 'trình chỉnh sửa.',
    ],
    examples: [
      'Đọc CV của tôi và đề xuất một phần tóm tắt ngắn gọn hơn.',
      'Tạo một bản CV tiếng Việt từ CV tiếng Anh của tôi. Giữ nguyên tên '
      + 'công ty, ngày tháng và số liệu.',
      'Viết lại các gạch đầu dòng ở công việc gần nhất để nêu kết quả '
      + 'trước, nhưng không thêm điều tôi chưa làm.',
    ],
    privacy: [
      'Bạn duyệt từng kết nối trên aboutme.vn. Trang cấp quyền ghi tên '
      + 'ứng dụng, nơi bạn sẽ quay lại, và hai quyền: Đọc CV và '
      + 'Chỉnh sửa CV.',
      'Chỉ cho phép yêu cầu do chính bạn vừa bắt đầu. Nếu trang cấp quyền '
      + 'mở ra khi bạn không kết nối gì, hãy chọn Từ chối.',
      'Trợ lý không bao giờ nhận mật khẩu hay phiên đăng nhập của bạn, '
      + 'chỉ nhận một mã truy cập riêng mà bạn thu hồi được.',
      'Nội dung trợ lý đọc được sẽ đến dịch vụ AI bạn chọn và theo chính '
      + 'sách của dịch vụ đó. Xem Chính sách quyền riêng tư.',
      'Thu hồi bất cứ lúc nào trong Cài đặt > Tác nhân đã kết nối. '
      + 'Yêu cầu tiếp theo của trợ lý sẽ bị từ chối; các thay đổi đã lưu '
      + 'vẫn còn.',
    ],
  },
  en: {
    web: [
      'In Claude, open Customize, then Connectors.',
      'Choose +, then Add custom connector.',
      'Name it aboutme.vn and paste https://aboutme.vn/mcp as the URL. '
      + 'Leave Advanced settings empty.',
      'Choose Add. If Claude shows Connect, choose it.',
      'An aboutme.vn page opens. Sign in if asked, check that the request '
      + 'names Claude and returns to claude.ai, then choose Approve.',
      'In a chat, choose +, then Connectors, and turn on aboutme.vn.',
    ],
    code: [
      'Add the server for all your projects:',
      'Sign in. On the aboutme.vn page, check that claude.ai is requesting '
      + 'access and approval returns to an app on this computer '
      + '(localhost), then choose Approve. You need to approve each time '
      + 'you sign in again.',
      'To remove it, run claude mcp remove aboutme, then revoke Claude '
      + 'Code\'s access in aboutme.vn Settings > Connected agents.',
    ],
    trouble: [
      'Claude says it could not connect.',
      'Check that the URL is exactly https://aboutme.vn/mcp, with no space '
      + 'or trailing slash. Remove the connector and add it again.',
      'The approval page says the request is invalid.',
      'The request expired or was already used. Start the connection '
      + 'again from your assistant.',
      'The page says you already have 10 connected agents.',
      'Revoke an old one in Settings, then connect again.',
      'It says there were too many attempts.',
      'aboutme.vn limits how often apps register and sign in. '
      + 'Wait up to an hour and try again.',
      'The assistant cannot create a resume.',
      'Each account holds at most three resumes. Delete one, or have '
      + 'the assistant edit an existing one.',
      'The assistant says the resume changed, or it cannot publish.',
      'The resume was edited somewhere else, such as the editor: '
      + 'ask the assistant to read it again and retry. Publishing '
      + 'is always yours to do in the editor.',
    ],
    examples: [
      'Read my resume and suggest a shorter summary.',
      'Make a Vietnamese copy of my English resume. Keep company '
      + 'names, dates, and numbers exactly.',
      'Rewrite the bullets in my latest job to lead with results, '
      + 'without adding anything I did not do.',
    ],
    privacy: [
      'You approve each connection on aboutme.vn. The approval page names '
      + 'the app, where you return, and two permissions: Read resumes '
      + 'and Write resumes.',
      'Approve only a request you just started. If an approval page opens '
      + 'when you did not connect anything, choose Deny.',
      'The assistant never gets your password or sign-in session, only '
      + 'its own access token, which you can revoke.',
      'Content the assistant reads goes to the AI service you chose, '
      + 'under that service\'s policy. See the Privacy Policy.',
      'Revoke any time in Settings > Connected agents. The assistant\'s '
      + 'next request is refused; changes it already saved stay.',
    ],
  },
} as const;

export const GUIDE_TYPE: readonly StyleRule[] = [
  {
    selector: '[data-testid="guide-claude"] li p, '
      + '[data-testid="guide-troubleshooting"] dd',
    message: 'Guide body typography',
    css: {
      'font-family': '"Be Vietnam Pro", Inter, system-ui, sans-serif',
      'font-size': '15px', 'font-weight': '400', 'line-height': '22.5px',
      'letter-spacing': 'normal', 'text-align': 'start',
      'text-transform': 'none',
    },
  },
  {
    selector: '[data-testid="guide-troubleshooting"] dt',
    message: 'Guide question typography',
    css: { 'font-size': '15px', 'font-weight': '600', 'line-height': '22.5px' },
  },
];

export async function expectGuideInstructions(
  page: Page, locale: 'vi' | 'en',
): Promise<void> {
  await expectGuideProse(page, locale);
  const copy = GUIDE_DETAILS[locale];
  const steps = page.getByTestId('guide-claude');
  await expect(steps.locator('ol').nth(0).locator('li > p'),
    'Guide web instruction order.').toHaveText([...copy.web]);
  await expect(steps.locator('ol').nth(1).locator('li > p, li > div > p'),
    'Guide code instruction order.').toHaveText([...copy.code]);
  await expect(page.getByTestId('guide-troubleshooting').locator('dt,dd'),
    'Guide troubleshooting order.').toHaveText([...copy.trouble]);
  await expect(page.getByTestId('guide-try-request').locator('li'),
    'Guide request order.').toHaveText([...copy.examples]);
  await expect(page.getByTestId('guide-privacy').locator('li'),
    'Guide privacy order.').toHaveText([...copy.privacy]);
}

export async function expectGuideLists(page: Page): Promise<void> {
  for (const [selector, count] of [
    ['[data-testid="guide-can"] ul', 5],
    ['[data-testid="guide-cannot"] ul', 5],
    ['[data-testid="guide-claude"] ol:first-of-type', 6],
    ['[data-testid="guide-claude"] ol:last-of-type', 3],
    ['[data-testid="guide-try-request"] ul', 3],
    ['[data-testid="guide-privacy"] ul', 5],
  ] as const) {
    const list = page.locator(selector);
    await expect(list, 'Guide list structure.').toMatchAriaSnapshot(
      '- list:\n' + Array.from({ length: count }, () => '  - listitem')
        .join('\n'),
    );
    await expect(list.getByRole('listitem'), 'Guide list structure.')
      .toHaveCount(count);
    await expect(list.locator(':scope > li'), 'Guide list structure.')
      .toHaveCount(count);
  }
}

export async function expectGuideInstructionIcons(
  page: Page, theme: 'light' | 'dark',
): Promise<void> {
  for (const [id, icon, color] of [
    ['guide-can', '.lucide-check', theme === 'light'
      ? 'rgb(35, 57, 154)'
      : 'rgb(163, 182, 245)'],
    ['guide-cannot', '.lucide-minus', theme === 'light'
      ? 'rgb(92, 97, 120)'
      : 'rgb(165, 171, 191)'],
  ] as const) {
    const icons = page.getByTestId(id).locator(icon);
    await expect(icons, 'Guide instruction icons.').toHaveCount(5);
    for (const glyph of await icons.all()) {
      await expect(glyph, 'Guide instruction icons.')
        .toHaveCSS('width', '16px');
      await expect(glyph, 'Guide instruction icons.')
        .toHaveCSS('height', '16px');
      await expect(glyph, 'Guide instruction icons.').toHaveCSS('color', color);
    }
  }
}

export async function expectGuideIcons(
  page: Page, theme: 'light' | 'dark',
): Promise<void> {
  for (const [id, paths, stroke] of [
    ['guide-can', ['M20 6 9 17l-5-5'], theme === 'light'
      ? 'rgb(35, 57, 154)'
      : 'rgb(163, 182, 245)'],
    ['guide-cannot', ['M5 12h14'], theme === 'light'
      ? 'rgb(92, 97, 120)'
      : 'rgb(165, 171, 191)'],
  ] as const) {
    await expectSvg(page.getByTestId(id).locator('svg'),
      lucideExpectation(paths, stroke), 'Guide SVG paint.');
  }
}

export async function expectGuideDetails(
  page: Page, locale: 'vi' | 'en', theme: 'light' | 'dark',
): Promise<void> {
  await expectGuideInstructions(page, locale);
  await expectGuideLists(page);
  await expectStyleRules(page, GUIDE_TYPE);
  await expectGuideIcons(page, theme);
  await expectGuideCodeScroll(page);
}

export async function expectGuideCodeScroll(page: Page): Promise<void> {
  const blocks = page.getByTestId('guide-mcp-page').locator('pre');
  const message = 'Guide code scrolling.';
  await expect(blocks, message).toHaveCount(3);
  for (const block of await blocks.all()) {
    await expect(block, message).toHaveCSS('overflow-x', 'auto');
    await expect.poll(() => visiblePaint(block), message).toBe(true);
    await expect.poll(() => block.evaluate((node) => {
      const left = node.scrollLeft;
      node.scrollLeft = node.scrollWidth;
      const reachesEnd = node.scrollLeft + node.clientWidth
        >= node.scrollWidth - 1;
      node.scrollLeft = left;
      return reachesEnd && node.scrollHeight <= node.clientHeight + 1;
    }), message).toBe(true);
  }
}
