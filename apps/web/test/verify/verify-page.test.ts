import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { setResponseHeader, setResponseStatus } from 'h3';
import VerifyPage from '../../app/pages/verify.vue';
import { setSiteLocale } from '../support/locale';

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

interface Fixture {
  readonly observed_at: string;
  readonly stale_after: string;
  readonly components: readonly {
    readonly running_images: readonly Record<string, unknown>[];
    readonly [key: string]: unknown;
  }[];
  readonly [key: string]: unknown;
}

const fixturesDir = resolve(
  import.meta.dirname,
  '../../e2e/fixtures/deployment',
);

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

function fixture(name: string): Fixture {
  return JSON.parse(
    readFileSync(resolve(fixturesDir, `${name}.json`), 'utf8'),
  ) as Fixture;
}

/** 30 seconds after `observed_at`: fresh under the 3-minute staleness rule. */
function freshDate(document: Fixture): string {
  return new Date(Date.parse(document.observed_at) + 30_000).toUTCString();
}

/** A minute after `stale_after`, so the staleness rule fires. */
function pastStaleDate(document: Fixture): string {
  return new Date(Date.parse(document.stale_after) + 60_000).toUTCString();
}

let mockResponse: { status: number; body: unknown; date: string } = {
  status: 200,
  body: fixture('verified'),
  date: freshDate(fixture('verified')),
};

registerEndpoint('/.well-known/deployment.json', (event) => {
  setResponseStatus(event, mockResponse.status);
  setResponseHeader(event, 'date', mockResponse.date);
  return mockResponse.body;
});

/** Serves a named fixture, fresh unless `date` or `status` overrides it. */
function serveFixture(
  name: string,
  overrides: { readonly status?: number; readonly date?: string } = {},
): Fixture {
  const document = fixture(name);
  mockResponse = {
    status: overrides.status ?? 200,
    body: document,
    date: overrides.date ?? freshDate(document),
  };
  return document;
}

function serveBody(body: unknown, status = 200): void {
  mockResponse = { status, body, date: new Date().toUTCString() };
}

const mounted: Wrapper[] = [];

async function mountVerify(): Promise<Wrapper> {
  const wrapper = await mountSuspended(VerifyPage);
  mounted.push(wrapper);
  await vi.waitFor(() => {
    expect(
      wrapper.get('[data-testid="verify-status"]').attributes('data-state'),
    ).not.toBe('loading');
  });
  return wrapper;
}

function status(wrapper: Wrapper) {
  return wrapper.get('[data-testid="verify-status"]');
}

function externalLinks(wrapper: Wrapper) {
  return wrapper.findAll('a[href^="https://"]');
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

describe('verify page states', () => {
  it('names the host reporter and its weaker trust in both languages',
    async () => {
      const base = fixture('unverified');
      const document = {
        ...base,
        platform: {
          provider: 'greennode', orchestrator: 'podman', region: 'HCM03',
        },
        components: base.components.map((component) => ({
          ...component,
          running_images: component.running_images.map((image) => ({
            ...image,
            signature: {
              status: 'unchecked', checked_at: null, signer_workflow: null,
              transparency_log_index: null,
            },
            sbom: { status: 'unchecked', format: null },
          })),
        })),
      };
      mockResponse = {
        status: 200, body: document, date: freshDate(document),
      };
      const vietnamese = await mountVerify();
      expect(status(vietnamese).attributes('data-state')).toBe('unverified');
      expect(status(vietnamese).get('[role="status"]').text()).toContain(
        'Máy chủ này chưa hỗ trợ kiểm tra chữ ký. Các digest đang chạy '
        + 'được hiển thị nhưng chưa được kiểm chứng.',
      );
      expect(status(vietnamese).get('[role="status"]').text())
        .not.toContain('Trang tự làm mới sau một phút.');
      expect(vietnamese.get('[data-step="image"]').text())
        .toContain('3 image, chữ ký chưa được kiểm chứng');
      expect(vietnamese.get('[data-step="image"]').text())
        .not.toContain('đã được GitHub ký');
      const limits = vietnamese.get('[data-testid="verify-limits"]');
      expect(limits.text()).toContain('Máy chủ GreenNode tự báo cáo');
      expect(limits.text()).toContain('Yếu hơn báo cáo từ API của ECS');
      expect(limits.text()).not.toContain('AWS');
      expect(vietnamese.get('[data-testid="verify-components"]').text())
        .toContain('sha256:');

      setSiteLocale('en');
      const english = await mountVerify();
      expect(status(english).get('[role="status"]').text()).toContain(
        'This host does not check signatures yet. The running digests '
        + 'are shown without verification.',
      );
      expect(status(english).get('[role="status"]').text())
        .not.toContain('The page refreshes in a minute.');
      expect(english.get('[data-step="image"]').text())
        .toContain('3 images, signatures not verified');
      expect(english.get('[data-step="image"]').text())
        .not.toContain('signed by GitHub');
      const englishLimits = english.get('[data-testid="verify-limits"]');
      expect(englishLimits.text())
        .toContain('The GreenNode host itself reports');
      expect(englishLimits.text()).toContain('weaker than an ECS API report');
      expect(englishLimits.text()).not.toContain('AWS');
    });

  it('keeps AWS wording for an ECS report and names Kubernetes separately',
    async () => {
      serveFixture('verified');
      setSiteLocale('en');
      const aws = await mountVerify();
      expect(aws.get('[data-testid="verify-limits"]').text())
        .toContain('AWS reports these exact digests');

      const document = {
        ...fixture('verified'),
        platform: {
          provider: 'greennode', orchestrator: 'kubernetes', region: 'HCM03',
        },
      };
      mockResponse = {
        status: 200, body: document, date: freshDate(document),
      };
      const kubernetes = await mountVerify();
      expect(kubernetes.get('[data-testid="verify-limits"]').text())
        .toContain('The Kubernetes API reports');
      expect(kubernetes.get('[data-testid="verify-limits"]').text())
        .not.toContain('AWS reports');
    });

  it('shows Loading before the fetch resolves, with no mark', async () => {
    serveFixture('verified');
    const wrapper = await mountSuspended(VerifyPage);
    mounted.push(wrapper);
    const card = status(wrapper);
    expect(card.attributes('data-state')).toBe('loading');
    expect(card.text()).toContain('Đang tải…');
    expect(card.find('svg').exists()).toBe(false);
  });

  it('shows Verified in Vietnamese and English, and only Verified shows it',
    async () => {
      serveFixture('verified');
      const wrapper = await mountVerify();
      const card = status(wrapper);
      expect(card.attributes('data-state')).toBe('verified');
      expect(wrapper.get('[data-step="image"]').text())
        .toContain('3 image, đã được GitHub ký');
      expect(card.text()).toContain('Đang chạy đúng mã nguồn trên GitHub');
      expect(card.text()).toContain(
        'Cả 3 thành phần chạy bản v0.6.0, do GitHub dựng và ký từ commit '
        + '91466ff.',
      );

      setSiteLocale('en');
      serveFixture('verified');
      const english = await mountVerify();
      const englishCard = status(english);
      expect(english.get('[data-step="image"]').text())
        .toContain('3 images, signed by GitHub');
      expect(englishCard.text()).toContain('Running exactly what\'s on GitHub');
      expect(englishCard.text()).toContain(
        'All 3 components run v0.6.0, built and signed by GitHub from commit '
        + '91466ff.',
      );
    });

  it('never shows the Verified title for any other fixture', async () => {
    for (const name of [
      'unverified', 'rolling-out', 'mismatch-not-found', 'mismatch-invalid',
      'mismatch-missing', 'mismatch-summary', 'stale',
    ]) {
      const document = fixture(name);
      serveFixture(
        name,
        name === 'stale' ? { date: pastStaleDate(document) } : {},
      );
      const wrapper = await mountVerify();
      expect(wrapper.text())
        .not.toContain('Đang chạy đúng mã nguồn trên GitHub');
      expect(status(wrapper).attributes('data-state')).not.toBe('verified');
    }
  });

  it('claims signed images only when every running signature is verified',
    async () => {
      for (const locale of ['vi', 'en'] as const) {
        setSiteLocale(locale);
        const signed = locale === 'vi'
          ? 'đã được GitHub ký' : 'signed by GitHub';
        for (const name of [
          'verified', 'rolling-out', 'unverified', 'mismatch-not-found',
          'mismatch-invalid',
        ]) {
          serveFixture(name);
          const wrapper = await mountVerify();
          expect(wrapper.get('[data-step="image"]').text().includes(signed))
            .toBe(name === 'verified' || name === 'rolling-out');
        }
        const base = fixture('verified');
        const document = {
          ...base,
          summary: 'unverified',
          release: null,
          components: [
            ...base.components,
            ...fixture('unverified').components
              .filter(({ name }) => name === 'maintenance'),
          ].map((component) => ({
            ...component,
            running_images: component.running_images.map((image) => ({
              ...image,
              signature: component.name === 'maintenance'
                ? { status: 'unchecked' } : image.signature,
            })),
          })),
        };
        mockResponse = {
          status: 200, body: document, date: freshDate(document),
        };
        const wrapper = await mountVerify();
        expect(wrapper.get('[data-step="image"]').text())
          .not.toContain(signed);
      }
    });

  it('shows Unavailable on a 404 and on a non-object body, hiding the chain '
    + 'and components cards', async () => {
    serveFixture('verified', { status: 404 });
    const wrapper = await mountVerify();
    const card = status(wrapper);
    expect(card.attributes('data-state')).toBe('unavailable');
    expect(card.text()).toContain(
      'Máy chủ này không công bố thông tin triển khai',
    );
    expect(wrapper.find('[data-testid="verify-chain"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="verify-components"]').exists())
      .toBe(false);

    setSiteLocale('en');
    serveBody([]);
    const arrayBody = await mountVerify();
    const arrayCard = status(arrayBody);
    expect(arrayCard.attributes('data-state')).toBe('unavailable');
    expect(arrayCard.text()).toContain(
      'This server publishes no deployment record',
    );
    expect(arrayBody.find('[data-testid="verify-chain"]').exists())
      .toBe(false);
  });

  it('shows Outdated with the Reload button first in tab order, '
    + 'hiding the chain and components cards', async () => {
    serveFixture('outdated');
    const wrapper = await mountVerify();
    const card = status(wrapper);
    expect(card.attributes('data-state')).toBe('outdated');
    expect(card.text()).toContain('Trang đã cũ. Hãy tải lại.');
    expect(wrapper.find('[data-testid="verify-chain"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="verify-components"]').exists())
      .toBe(false);

    const focusable = wrapper.findAll('button, a[href]');
    const button = card.get('button');
    expect(button.text()).toBe('Tải lại');
    expect(focusable[0]?.element).toBe(button.element);
  });

  it('shows Stale with the last-known values and no refresh note', async () => {
    const document = fixture('stale');
    serveFixture('stale', { date: pastStaleDate(document) });
    const wrapper = await mountVerify();
    const card = status(wrapper);
    expect(card.attributes('data-state')).toBe('stale');
    expect(card.text()).toContain('Chưa kiểm tra lại từ');
    expect(card.text()).not.toContain('Tự làm mới mỗi phút');
    expect(card.text()).toContain('Kiểm tra lần cuối lúc');

    const chain = wrapper.get('[data-testid="verify-chain"]');
    const chips = chain.findAll('[data-chip]')
      .map((chip) => chip.attributes('data-chip'));
    expect(chips).toEqual([
      'not_rechecked', 'not_rechecked', 'not_rechecked', 'not_rechecked',
    ]);
    const components = wrapper.get('[data-testid="verify-components"]');
    expect(components.text()).toContain('Chưa kiểm tra chữ ký');
  });

  it('shows Unverified naming the unchecked component', async () => {
    serveFixture('unverified');
    const wrapper = await mountVerify();
    const card = status(wrapper);
    expect(card.attributes('data-state')).toBe('unverified');
    expect(card.text()).toContain(
      'Chưa kiểm tra xong chữ ký của caddy. Trang tự làm mới sau một phút.',
    );
    expect(wrapper.get('[data-step="image"]').text())
      .not.toContain('đã được GitHub ký');
  });

  it('keeps pending signature wording for ECS and Kubernetes in both '
    + 'languages', async () => {
    for (const orchestrator of ['ecs', 'kubernetes']) {
      const base = fixture('unverified');
      const document = {
        ...base,
        platform: { provider: 'aws', orchestrator, region: 'ap-southeast-1' },
      };
      mockResponse = {
        status: 200, body: document, date: freshDate(document),
      };
      for (const locale of ['vi', 'en'] as const) {
        setSiteLocale(locale);
        const wrapper = await mountVerify();
        expect(status(wrapper).text()).toContain(locale === 'vi'
          ? 'Chưa kiểm tra xong chữ ký của caddy. '
            + 'Trang tự làm mới sau một phút.'
          : 'The signature check for caddy hasn\'t finished. '
            + 'The page refreshes in a minute.');
        expect(wrapper.get('[data-step="image"]').text())
          .not.toContain(locale === 'vi'
            ? 'đã được GitHub ký' : 'signed by GitHub');
      }
    }
  });

  it('shows Rolling out with its pill and the Updating chain', async () => {
    serveFixture('rolling-out');
    const wrapper = await mountVerify();
    const card = status(wrapper);
    expect(card.attributes('data-state')).toBe('rolling_out');
    expect(card.text()).toContain(
      'web đang chuyển từ v0.5.21 sang v0.6.0. Cả hai bản đều đã được ký.',
    );
    expect(card.text()).toContain('v0.5.21');
    expect(card.text()).toContain('v0.6.0');

    const chain = wrapper.get('[data-testid="verify-chain"]');
    const chips = chain.findAll('[data-chip]')
      .map((chip) => chip.attributes('data-chip'));
    expect(chips).toEqual(['match', 'match', 'match', 'updating']);
    const connectors = chain.findAll('[data-connector]')
      .map((connector) => connector.attributes('data-connector'));
    expect(connectors).toEqual(['agree', 'agree', 'updating']);
  });

  it.each([
    ['mismatch-not-found', 'web', 'web không có chữ ký'],
    ['mismatch-invalid', 'server', 'Chữ ký sai'],
    ['mismatch-missing', 'caddy', 'caddy không chạy'],
  ] as const)(
    'shows Mismatch for %s with a Failed chip naming %s',
    async (name, _component, failedLabel) => {
      serveFixture(name);
      const wrapper = await mountVerify();
      const card = status(wrapper);
      expect(card.attributes('data-state')).toBe('mismatch');
      expect(card.text()).toContain('Có thành phần không khớp với bản dựng '
        + 'đã ký');

      const chain = wrapper.get('[data-testid="verify-chain"]');
      const failed = chain.get('[data-chip="failed"]');
      expect(failed.text()).toContain(failedLabel);
    },
  );

  it('shows Mismatch for a summary disagreement with no failed chip',
    async () => {
      serveFixture('mismatch-summary');
      const wrapper = await mountVerify();
      const card = status(wrapper);
      expect(card.attributes('data-state')).toBe('mismatch');
      expect(card.text()).toContain('Bản tóm tắt không khớp với từng thành '
        + 'phần.');
      const chain = wrapper.get('[data-testid="verify-chain"]');
      expect(chain.find('[data-chip="failed"]').exists()).toBe(false);
      const chips = chain.findAll('[data-chip]')
        .map((chip) => chip.attributes('data-chip'));
      expect(chips).toEqual([
        'not_verified', 'not_verified', 'not_verified', 'not_verified',
      ]);
    });
});

describe('verify page commands', () => {
  it('shows live command values with the full digest and version', async () => {
    serveFixture('verified');
    const wrapper = await mountVerify();
    const command2 = wrapper.get('[data-testid="verify-command-2"]');
    expect(command2.text()).toContain(
      'fdafc11f142a7df36d8182dc9fca9cfc0121adeaf2265e2336961c45c46f8181',
    );
    expect(command2.text()).toContain('v0.6.0');
    expect(command2.text()).toContain('aboutme-server');
  });

  it('uses the caddy image name for a maintenance command target', async () => {
    serveFixture('unverified');
    const wrapper = await mountVerify();
    const option = wrapper.findAll('[data-testid="verify-selector"] button')
      .find((button) => button.text() === 'maintenance');
    expect(option).toBeDefined();
    await option!.trigger('click');
    await vi.waitFor(() => {
      expect(wrapper.get('[data-testid="verify-command-2"]').text())
        .toContain('aboutme-caddy');
    });
  });

  it('shows placeholders and hides the selector when Unavailable', async () => {
    serveFixture('verified', { status: 404 });
    const wrapper = await mountVerify();
    expect(wrapper.find('[data-testid="verify-selector"]').exists())
      .toBe(false);
    const command2 = wrapper.get('[data-testid="verify-command-2"]');
    expect(command2.text()).toContain('<digest>');
    expect(command2.text()).toContain('<version>');
  });

  it('links to the JSON document with the right href, and never widens the '
    + 'page with an external request', async () => {
    serveFixture('verified');
    const wrapper = await mountVerify();
    const jsonLink = wrapper.get('[data-testid="verify-json-link"]');
    expect(jsonLink.attributes('href')).toBe(
      '/.well-known/deployment.json',
    );
    for (const link of externalLinks(wrapper)) {
      expect(link.attributes('rel')).toBe('noopener noreferrer');
    }
  });

  it('names the digest copy button and copies the full digest', async () => {
    serveFixture('verified');
    const wrapper = await mountVerify();
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard(writeText);

    const copyButton = wrapper.get(
      '[aria-label="Sao chép mã băm của server"]',
    );
    await copyButton.trigger('click');
    expect(writeText).toHaveBeenCalledWith(
      'sha256:fdafc11f142a7df36d8182dc9fca9cfc0121adeaf2265e2336961c45c46f8181',
    );
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('Đã sao chép');
    });
  });

  it('announces the failure sentence when the clipboard write rejects',
    async () => {
      serveFixture('verified');
      const wrapper = await mountVerify();
      const writeText = vi.fn().mockRejectedValue(new Error('denied'));
      stubClipboard(writeText);

      const copyButton = wrapper.get(
        '[aria-label="Sao chép mã băm của server"]',
      );
      await copyButton.trigger('click');
      await vi.waitFor(() => {
        expect(wrapper.text()).toContain(
          'Không sao chép được. Hãy tự chọn và sao chép.',
        );
      });
    });

  it('copies a command without its $ prompt', async () => {
    serveFixture('verified');
    const wrapper = await mountVerify();
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard(writeText);

    const command1 = wrapper.get('[data-testid="verify-command-1"]');
    await command1.get('[aria-label="Sao chép lệnh 1"]').trigger('click');
    const copied = writeText.mock.calls[0]?.[0] as string;
    // The jq filter's `$n` stays; only the `$ ` prompt is left out.
    expect(copied.startsWith('curl -fsS https://aboutme.vn/')).toBe(true);
    expect(copied).not.toContain('$ ');
  });
});
