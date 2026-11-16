import { expect, type Locator, type Page } from '@playwright/test';

async function expectDeclaration(
  target: Locator, property: string, value: string, message: string,
): Promise<void> {
  await expect.poll(() => target.evaluate((node, input) => {
    const reference = document.createElement('div');
    reference.style.setProperty(input.property, input.value, 'important');
    document.body.append(reference);
    const expected = getComputedStyle(reference)
      .getPropertyValue(input.property);
    reference.remove();
    const actual = getComputedStyle(node).getPropertyValue(input.property);
    const paintedShadows = (value: string) =>
      (value.match(/(?:[^,(]|\([^)]*\))+/gu) ?? [])
        .map((shadow) => shadow.trim())
        // Tailwind composes four transparent, zero-size ring placeholders.
        .filter((shadow) =>
          shadow !== 'rgba(0, 0, 0, 0) 0px 0px 0px 0px').join(', ');
    return input.property === 'box-shadow'
      ? paintedShadows(actual) === paintedShadows(expected)
      : actual === expected;
  }, { property, value }), message).toBe(true);
}

export async function expectChromeDecoration(
  page: Page, theme: 'light' | 'dark', kind?: 'shadow' | 'panels',
): Promise<void> {
  if (kind !== 'panels') {
    await expectDeclaration(page.getByTestId('landing-sheet'), 'box-shadow',
      '0 36px 56px -28px rgba(0,0,0,0.5), 0 2px 6px rgba(0,0,0,0.16)',
      'Chrome paper shadow.');
  }
  if (kind !== 'shadow') {
    const panels = theme === 'light'
      ? [['landing-hero', '#594bad'], ['landing-editor', '#f1effa'],
          ['landing-publish', '#1a1650'], ['landing-ai', '#f1effa'],
          ['landing-data', '#fff2c2'], ['landing-maker', '#ece2ff'],
          ['landing-footer', '#1a1650']]
      : [['landing-hero', '#382961'], ['landing-editor', '#151015'],
          ['landing-publish', '#25153a'], ['landing-ai', '#151015'],
          ['landing-data', '#271a07'], ['landing-maker', '#1c172f'],
          ['landing-footer', '#150c1c']];
    for (const [id, color] of panels) {
      await expectDeclaration(page.getByTestId(id!), 'background-color',
        color!, 'Chrome panel colors.');
    }
    await expectDeclaration(page.getByTestId('landing-hero'), 'color',
      theme === 'light' ? '#ffffff' : '#e7dfe8', 'Chrome panel colors.');
    await expectDeclaration(page.getByTestId('landing-template-fan'),
      'background-color', theme === 'light' ? '#ece2ff' : '#1c172f',
      'Chrome panel colors.');
  }
}

export async function expectChromeFan(page: Page): Promise<void> {
  const fan = page.getByTestId('landing-template-fan');
  await expect.poll(() => fan.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return Array.from(element.querySelectorAll('img'), (image) => {
      const matrix = new DOMMatrixReadOnly(getComputedStyle(image).transform);
      return {
        angle: Math.round(Math.atan2(matrix.b, matrix.a) * 180 / Math.PI),
        width: Math.abs(image.offsetWidth - box.width * 0.4) <= 1,
      };
    });
  }), 'Chrome fan geometry.').toEqual([-9, -3, 3, 9].map((angle) =>
    ({ angle, width: true })));
}

export async function expectChromeNavigation(
  page: Page, locale: 'vi' | 'en', width: number, templates: boolean,
): Promise<void> {
  const nav = page.getByTestId('app-shell').getByRole('navigation');
  const names = locale === 'vi'
    ? ['Thư viện', 'Cộng đồng', 'Kết nối AI', 'Mã nguồn mở']
    : ['Library', 'Community', 'Connect AI', 'Open source'];
  const message = 'Chrome navigation structure.';
  await expect(nav, message).toHaveCount(1);
  await expect(nav, message).toHaveAccessibleName(locale === 'vi'
    ? 'Điều hướng chính'
    : 'Primary navigation');
  await expect(nav.locator('a'), message).toHaveCount(4);
  const visible = names.slice(0, width < 704 ? 2 : width < 1152 ? 3 : 4);
  await expect(nav.getByRole('link'), message).toHaveCount(visible.length);
  await expect(nav, message).toMatchAriaSnapshot('- navigation:\n'
    + visible.map((name) => `  - link "${name}"`).join('\n'));
  const links = ['/templates', '/showcase', '/guide/mcp',
    'https://github.com/dannyota/aboutme'];
  for (let index = 0; index < links.length; index++) {
    await expect(nav.locator('a').nth(index), message)
      .toHaveAttribute('href', links[index]!);
    if (templates && index === 0) {
      await expect(nav.locator('a').nth(index), message)
        .toHaveAttribute('aria-current', 'page');
    } else {
      await expect(nav.locator('a').nth(index), message)
        .not.toHaveAttribute('aria-current');
    }
  }
}

export async function expectChromeList(page: Page): Promise<void> {
  const list = page.getByTestId('landing-template-list');
  const message = 'Chrome template list structure.';
  await expect(list, message).toMatchAriaSnapshot(
    '- list:\n' + Array.from({ length: 4 }, () =>
      '  - listitem:\n    - link').join('\n'),
  );
  await expect(list.getByRole('listitem'), message).toHaveCount(4);
  await expect(list.getByRole('link'), message).toHaveCount(4);
  for (const [index, [id, name]] of [
    ['ats-plain', 'ATS Plain'], ['engineer-compact', 'Engineer Compact'],
    ['graduate-friendly', 'Graduate Friendly'],
    ['executive-band', 'Executive Band'],
  ].entries()) {
    const link = list.getByRole('link').nth(index);
    await expect(link, message).toHaveAccessibleName(new RegExp(name!, 'u'));
    await expect(link, message).toHaveAttribute('href', `/templates/${id}`);
  }
}

export async function expectSampleIsolation(page: Page): Promise<void> {
  const sample = page.getByTestId('landing-sample').locator('.resume-document');
  const message = 'Chrome sample isolation.';
  await expect.poll(() => sample.evaluate((root) => {
    const item = root.querySelector('li');
    return [root, item].map((node) => {
      const css = getComputedStyle(node!);
      return [css.fontWeight, css.letterSpacing, css.textTransform];
    });
  }), message).toEqual([
    ['400', 'normal', 'none'], ['400', 'normal', 'none'],
  ]);
  const link = sample.locator('a[href]').first();
  const before = await link.evaluate((node) =>
    getComputedStyle(node).borderRadius);
  await link.focus();
  await expect.poll(() => link.evaluate((node) => {
    const css = getComputedStyle(node);
    return {
      visible: node.matches(':focus-visible'),
      style: css.outlineStyle,
      radius: css.borderRadius,
    };
  }), message).toEqual({ visible: true, style: 'auto', radius: before });
  await link.blur();
}

export async function expectRendererBoundary(page: Page): Promise<void> {
  const text = page.locator('.resume-document p').first();
  const message = 'Chrome renderer boundary.';
  await expect(text, message).toBeVisible();
  const read = await text.evaluate((node) => {
    const selection = (element: Element) => {
      const css = getComputedStyle(element, '::selection');
      return { background: css.backgroundColor, color: css.color };
    };
    const probe = document.createElement('span');
    probe.style.backgroundColor = 'var(--coin)';
    probe.style.color = 'var(--control)';
    document.documentElement.append(probe);
    const coin = getComputedStyle(probe).backgroundColor;
    const control = getComputedStyle(probe).color;
    probe.remove();
    const chrome = Array.from(document.querySelectorAll('button')).find(
      (button) => !button.closest('.resume-document, .paged-resume'),
    )!;
    const root = node.closest('.paged-resume')
      ?? node.closest('.resume-document')!;
    return {
      coin,
      control,
      chromeSelection: selection(chrome).background,
      chromeScrollbar: getComputedStyle(chrome).scrollbarColor,
      browserBackground: selection(document.documentElement).background,
      selections: [node, root].map((element) => ({
        ...selection(element),
        originating: getComputedStyle(element).color,
      })),
      scrollbar: [node, root].map((element) =>
        getComputedStyle(element).scrollbarColor),
      weight: getComputedStyle(root).fontWeight,
    };
  });
  expect(read.chromeSelection, message).toBe(read.coin);
  expect(read.chromeScrollbar, message).toContain(read.control);
  for (const selection of read.selections) {
    expect(selection.background, message).toBe(read.browserBackground);
    expect(selection.background, message).not.toBe(read.coin);
    expect(selection.color, message).toBe(selection.originating);
  }
  expect(read.scrollbar, message).toEqual(['auto', 'auto']);
  expect(read.weight, message).toBe('400');
}

export async function leakChromeIntoResume(
  page: Page, kind: 'selection' | 'scrollbar',
): Promise<() => Promise<void>> {
  const changed = await page.evaluate((input) => {
    const root = document.querySelector('.paged-resume, .resume-document')!;
    const ancestor = root.parentElement!;
    ancestor.setAttribute('data-e2e-boundary-leak', '');
    const sheet = document.createElement('style');
    sheet.dataset.e2eBoundaryLeak = '';
    const leak = '[data-e2e-boundary-leak]';
    const selectionLeak = `${leak}::selection { background-color: `
      + 'var(--coin); color: var(--coin-ink); }';
    const scrollbarLeak = `${leak} { scrollbar-color: var(--control) `
      + `transparent; } ${leak} > :is(.resume-document, .paged-resume) { `
      + 'scrollbar-color: inherit !important; }';
    sheet.textContent = input === 'selection' ? selectionLeak : scrollbarLeak;
    document.head.append(sheet);
    if (input === 'scrollbar') {
      return getComputedStyle(root).scrollbarColor !== 'auto';
    }
    const probe = document.createElement('span');
    probe.style.backgroundColor = 'var(--coin)';
    document.documentElement.append(probe);
    const coin = getComputedStyle(probe).backgroundColor;
    probe.remove();
    const text = root.querySelector('p') ?? root;
    return getComputedStyle(text, '::selection').backgroundColor === coin;
  }, kind);
  expect(changed, 'Boundary leak mutation takes effect.').toBe(true);
  return async () => {
    await page.evaluate(() => {
      document.querySelector('style[data-e2e-boundary-leak]')?.remove();
      document.querySelector('[data-e2e-boundary-leak]')
        ?.removeAttribute('data-e2e-boundary-leak');
    });
  };
}

export async function expectAuthArtworkClear(page: Page): Promise<void> {
  const message = 'Chrome auth artwork clearance.';
  const panel = page.getByTestId('auth-brand-panel');
  await expect(panel, message).toBeVisible();
  const read = await panel.evaluate((element) => {
    const box = (node: Element | null) => {
      const rect = node!.getBoundingClientRect();
      return {
        left: rect.left, right: rect.right, top: rect.top,
        bottom: rect.bottom,
      };
    };
    const points = element.querySelectorAll('li');
    return {
      point: box(points[points.length - 1]!),
      art: [
        box(element.querySelector('[data-auth-sheet]')),
        box(element.querySelector('[data-auth-lock]')),
      ],
      panelRight: element.getBoundingClientRect().right,
      viewport: document.documentElement.clientWidth,
      overflow: document.documentElement.scrollWidth
        - document.documentElement.clientWidth,
    };
  });
  for (const art of read.art) {
    const intersects = art.left < read.point.right
      && read.point.left < art.right
      && art.top < read.point.bottom
      && read.point.top < art.bottom;
    expect(intersects, message).toBe(false);
  }
  expect(read.panelRight, message).toBeLessThanOrEqual(read.viewport);
  expect(read.overflow, message).toBeLessThanOrEqual(0);
}

export async function placeAuthArtworkOverPoints(
  page: Page,
): Promise<() => Promise<void>> {
  const moved = await page.getByTestId('auth-brand-panel').evaluate(
    (panel) => {
      const art = panel.querySelector<HTMLElement>('[data-auth-illustration]')!;
      const points = panel.querySelectorAll('li');
      const last = points[points.length - 1]!;
      const before = art.getAttribute('style');
      art.dataset.e2eStyleBefore = before ?? '';
      art.style.position = 'absolute';
      art.style.margin = '0';
      art.style.left = `${last.offsetLeft}px`;
      art.style.top = `${last.offsetTop - 40}px`;
      const sheet = art.querySelector('[data-auth-sheet]')!
        .getBoundingClientRect();
      const point = last.getBoundingClientRect();
      return sheet.top < point.bottom && point.top < sheet.bottom;
    },
  );
  expect(moved, 'Auth artwork mutation covers the last point.').toBe(true);
  return async () => {
    await page.getByTestId('auth-brand-panel').evaluate((panel) => {
      const art = panel.querySelector<HTMLElement>('[data-auth-illustration]')!;
      const before = art.dataset.e2eStyleBefore ?? '';
      delete art.dataset.e2eStyleBefore;
      if (before === '') art.removeAttribute('style');
      else art.setAttribute('style', before);
    });
  };
}
