import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import GuidePage from '../../app/pages/guide/mcp.vue';
import {
  addServerCommand,
  loginCommand,
  mcpServerUrl,
} from '../../app/i18n/guide';
import { setSiteLocale } from '../support/locale';

// docs/design/mcp-guide.md: /guide/mcp is server-rendered, reads no API, and
// shows the same content signed in or out.

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

const clipboardDescriptor = Object.getOwnPropertyDescriptor(
  navigator,
  'clipboard',
);

function stubClipboard(writeText: (text: string) => Promise<void>): void {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  });
}

function restoreClipboard(): void {
  if (clipboardDescriptor === undefined) {
    Reflect.deleteProperty(navigator, 'clipboard');
  } else {
    Object.defineProperty(navigator, 'clipboard', clipboardDescriptor);
  }
}

const mounted: Wrapper[] = [];

async function mountGuide(): Promise<Wrapper> {
  const wrapper = await mountSuspended(GuidePage);
  mounted.push(wrapper);
  return wrapper;
}

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  restoreClipboard();
  vi.restoreAllMocks();
});

describe('guide mcp page structure', () => {
  it('keeps one h1, h2 per section, and h3 only inside Connect Claude',
    async () => {
      const wrapper = await mountGuide();
      const headings = wrapper.findAll('h1, h2, h3')
        .map((heading) => heading.element.tagName.toLowerCase());
      expect(headings).toEqual([
        'h1', 'h2', 'h2', 'h3', 'h3', 'h2', 'h2', 'h2', 'h2',
      ]);
      expect(wrapper.get('[data-page-title]').element.tagName)
        .toBe('H1');
    });

  it('renders in the site language, Vietnamese by default', async () => {
    const wrapper = await mountGuide();
    expect(wrapper.get('h1').text())
      .toBe('Kết nối trợ lý AI với aboutme.vn');

    setSiteLocale('en');
    const english = await mountGuide();
    expect(english.get('h1').text())
      .toBe('Connect your AI assistant to aboutme.vn');
    expect(english.text()).toContain('Add custom connector');
  });

  it('numbers each Claude step list from 1', async () => {
    const wrapper = await mountGuide();
    const lists = wrapper.get('[data-testid="guide-claude"]').findAll('ol');
    const numbers = lists.map((list) => list.findAll('li')
      .map((item) => item.get('.guide-disk').text()));
    expect(numbers).toEqual([
      ['1', '2', '3', '4', '5', '6'],
      ['1', '2', '3'],
    ]);
  });

  it('shows the exact server URL and Claude Code commands', async () => {
    const wrapper = await mountGuide();
    expect(mcpServerUrl).toBe('https://aboutme.vn/mcp');
    expect(wrapper.get('[data-testid="guide-url"]').text())
      .toContain(mcpServerUrl);
    expect(wrapper.get('[data-testid="guide-command-add"]').text())
      .toContain(addServerCommand);
    expect(addServerCommand).toBe(
      'claude mcp add --transport http --scope user aboutme '
      + 'https://aboutme.vn/mcp',
    );
    expect(wrapper.get('[data-testid="guide-command-login"]').text())
      .toContain(loginCommand);
    expect(loginCommand).toBe('claude mcp login aboutme');
  });

  it('names the copy buttons in Vietnamese and English', async () => {
    const wrapper = await mountGuide();
    expect(wrapper.findAll('[aria-label="Sao chép địa chỉ"]')).toHaveLength(1);
    expect(wrapper.findAll('[aria-label="Sao chép lệnh"]')).toHaveLength(2);

    setSiteLocale('en');
    const english = await mountGuide();
    expect(english.findAll('[aria-label="Copy URL"]')).toHaveLength(1);
    expect(english.findAll('[aria-label="Copy command"]')).toHaveLength(2);
  });

  it('keeps one polite live region', async () => {
    const wrapper = await mountGuide();
    const regions = wrapper.findAll('[role="status"]');
    expect(regions).toHaveLength(1);
    expect(regions[0]?.attributes('aria-live')).toBe('polite');
  });

  it('makes every scrolling block focusable, named, and untranslated',
    async () => {
      const wrapper = await mountGuide();
      const blocks = wrapper.findAll('pre');
      expect(blocks.length).toBeGreaterThanOrEqual(3);
      for (const block of blocks) {
        expect(block.attributes('tabindex')).toBe('0');
        expect(block.attributes('aria-label')?.length).toBeGreaterThan(0);
        const code = block.find('code');
        expect(code.attributes('translate')).toBe('no');
      }
    });

  it('opens external links in the same tab with rel noopener noreferrer',
    async () => {
      const wrapper = await mountGuide();
      const external = wrapper.findAll('a[href^="http"]');
      expect(external.length).toBeGreaterThan(0);
      for (const link of external) {
        expect(link.attributes('rel')).toBe('noopener noreferrer');
        expect(link.attributes('target')).toBeUndefined();
      }
    });

  it('links Create an account, sign in, Privacy Policy, and Settings '
    + 'internally with no rel', async () => {
    const wrapper = await mountGuide();
    expect(wrapper.get('a[href="/register"]').attributes('rel'))
      .toBeUndefined();
    expect(wrapper.get('a[href="/login"]').attributes('rel')).toBeUndefined();
    expect(wrapper.get('a[href="/privacy"]').attributes('rel'))
      .toBeUndefined();
    expect(wrapper.get('a[href="/app/settings/sessions"]').attributes('rel'))
      .toBeUndefined();
  });

  it('makes no data fetch', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    await mountGuide();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});

describe('guide mcp page copy buttons', () => {
  it('copies the server URL and announces it', async () => {
    const wrapper = await mountGuide();
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard(writeText);

    await wrapper.get('[aria-label="Sao chép địa chỉ"]').trigger('click');
    expect(writeText).toHaveBeenCalledWith(mcpServerUrl);
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('Đã sao chép địa chỉ máy chủ MCP.');
    });
  });

  it('copies each command without the $ prompt', async () => {
    const wrapper = await mountGuide();
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard(writeText);

    const addBlock = wrapper.get('[data-testid="guide-command-add"]');
    await addBlock.get('[aria-label="Sao chép lệnh"]').trigger('click');
    expect(writeText).toHaveBeenCalledWith(addServerCommand);
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('Đã sao chép lệnh.');
    });
  });

  it('announces the failure sentence when the clipboard write rejects',
    async () => {
      const wrapper = await mountGuide();
      const writeText = vi.fn().mockRejectedValue(new Error('denied'));
      stubClipboard(writeText);

      await wrapper.get('[aria-label="Sao chép địa chỉ"]').trigger('click');
      await vi.waitFor(() => {
        expect(wrapper.text()).toContain(
          'Không sao chép được. Hãy chọn địa chỉ và tự sao chép.',
        );
      });
    });

  it.each([
    [
      'Vietnamese add command',
      undefined,
      'Sao chép lệnh',
      'guide-command-add',
      'Không sao chép được. Hãy chọn lệnh và tự sao chép.',
    ],
    [
      'Vietnamese login command',
      undefined,
      'Sao chép lệnh',
      'guide-command-login',
      'Không sao chép được. Hãy chọn lệnh và tự sao chép.',
    ],
    [
      'English add command',
      'en',
      'Copy command',
      'guide-command-add',
      'Could not copy. Select the command and copy it yourself.',
    ],
    [
      'English login command',
      'en',
      'Copy command',
      'guide-command-login',
      'Could not copy. Select the command and copy it yourself.',
    ],
  ] as const)('announces the command failure sentence for %s',
    async (_localeName, locale, copyLabel, testid, expected) => {
      setSiteLocale(locale);
      const wrapper = await mountGuide();
      const writeText = vi.fn().mockRejectedValue(new Error('denied'));
      stubClipboard(writeText);

      const command = wrapper.get(`[data-testid="${testid}"]`);
      await command.get(`[aria-label="${copyLabel}"]`).trigger('click');
      await vi.waitFor(() => {
        expect(wrapper.text()).toContain(expected);
      });
    });
});
