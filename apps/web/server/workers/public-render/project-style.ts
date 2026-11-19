interface ProjectPageTokens {
  readonly ground: string;
  readonly card: string;
  readonly ink: string;
  readonly muted: string;
  readonly link: string;
  readonly primary: string;
  readonly primaryHover: string;
  readonly onPrimary: string;
  readonly chip: string;
  readonly hover: string;
  readonly focus: string;
  readonly border: string;
  readonly hairline: string;
}

export const PROJECT_PAGE_TOKENS: Readonly<
  Record<'light' | 'dark', ProjectPageTokens>
> = {
  light: {
    ground: '#ffffff',
    card: '#ffffff',
    ink: '#1a1650',
    muted: '#55517d',
    link: '#4336c9',
    primary: '#594bad',
    primaryHover: '#4c3c9c',
    onPrimary: '#ffffff',
    chip: '#f1effa',
    hover: '#f1effa',
    focus: '#1a1650',
    border: '#e3e0f2',
    hairline: 'rgba(26, 22, 80, 0.14)',
  },
  dark: {
    ground: '#0e0a0d',
    card: '#1b1419',
    ink: '#d3cad0',
    muted: '#a3959f',
    link: '#b59bdb',
    primary: '#7267c0',
    primaryHover: '#675bb3',
    onPrimary: '#ffffff',
    chip: '#151015',
    hover: '#251b23',
    focus: '#cdbcd9',
    border: '#2e242c',
    hairline: 'rgba(211, 202, 208, 0.14)',
  },
};

interface BarTokens {
  readonly ground: string;
  readonly border: string;
  readonly mark: string;
  readonly credit: string;
  readonly creditHover: string;
  readonly buttonBorder: string;
  readonly buttonLabel: string;
  readonly buttonHoverFill: string;
  readonly focusRing: string;
}

const BAR_TOKENS: Readonly<Record<'light' | 'dark', BarTokens>> = {
  light: {
    ground: '#ffffff',
    border: '#e3e0f2',
    mark: '#de1b5a',
    credit: '#55517d',
    creditHover: '#4336c9',
    buttonBorder: '#1a1650',
    buttonLabel: '#1a1650',
    buttonHoverFill: '#f1effa',
    focusRing: '#1a1650',
  },
  dark: {
    ground: '#0e0a0d',
    border: '#2e242c',
    mark: '#e2788f',
    credit: '#a3959f',
    creditHover: '#b59bdb',
    buttonBorder: '#d3cad0',
    buttonLabel: '#d3cad0',
    buttonHoverFill: '#251b23',
    focusRing: '#cdbcd9',
  },
};

const customProperties = (
  prefix: string, tokens: ProjectPageTokens | BarTokens,
): string =>
  Object.entries(tokens)
    .map(([role, value]) =>
      `--${prefix}-${role.replaceAll(/[A-Z]/gu, (letter) =>
        `-${letter.toLowerCase()}`)}:${value};`)
    .join('');

const tokenDeclarations = (tokens: ProjectPageTokens): string =>
  customProperties('project', tokens);

const barDeclarations = (tokens: BarTokens): string =>
  customProperties('toolbar', tokens);

const FONT = '"Be Vietnam Pro",Inter,system-ui,sans-serif';
const PAGE = '.public-project-page';

const rules = (
  selectors: string | readonly string[],
  ...declarations: string[]
): string =>
  `${(typeof selectors === 'string' ? selectors.split(',') : selectors)
    .map((selector) => `${PAGE} ${selector}`)
    .join(',')}{${declarations.join('')}}`;

const rule = (selectors: string, ...declarations: string[]): string =>
  `${selectors}{${declarations.join('')}}`;

const media = (query: string, ...blocks: string[]): string =>
  `@media ${query}{${blocks.join('')}}`;

const SCHEME = [
  rule(':root', 'color-scheme:light;background:var(--project-ground);',
    tokenDeclarations(PROJECT_PAGE_TOKENS.light)),
  rules('.public-toolbar', barDeclarations(BAR_TOKENS.light)),
  media('screen',
    rule(':root[data-scheme="dark"]', 'color-scheme:dark;',
      tokenDeclarations(PROJECT_PAGE_TOKENS.dark)),
    rule(':root[data-scheme="dark"] .public-toolbar',
      barDeclarations(BAR_TOKENS.dark))),
  media('screen and (prefers-color-scheme:dark)',
    rule(':root[data-scheme="system"]', 'color-scheme:dark;',
      tokenDeclarations(PROJECT_PAGE_TOKENS.dark)),
    rule(':root[data-scheme="system"] .public-toolbar',
      barDeclarations(BAR_TOKENS.dark))),
];

const FRAME = [
  rule('body', 'margin:0;'),
  rule(PAGE, 'box-sizing:border-box;min-height:100vh;',
    'background:var(--project-ground);color:var(--project-ink);',
    `font-family:${FONT};font-size:15px;line-height:1.6;`,
    'overflow-wrap:anywhere;--project-gutter:16px;',
    '--project-radius-card:22px;'),
  rule(`${PAGE}-list`, '--project-measure:44rem;'),
  rule(`${PAGE}-detail`, '--project-measure:59rem;'),
  rules('*,*::before,*::after', 'box-sizing:border-box;'),
  rules('a', 'color:var(--project-link);text-decoration:underline;',
    'text-underline-offset:3px;'),
  rules('a:focus-visible', 'outline:2px solid var(--project-focus);',
    'outline-offset:2px;'),
  rules('p', 'margin:0;'),
  rules('h1', 'margin:0;color:var(--project-ink);font-size:24px;',
    'line-height:1.3;font-weight:700;'),
  rules('h2', 'margin:0;color:var(--project-ink);font-size:20px;',
    'line-height:1.3;font-weight:600;'),
  rules('a[href="#public-project"]:not(:focus)', 'position:absolute;',
    'width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;',
    'clip-path:inset(50%);white-space:nowrap;border:0;'),
  rules('a[href="#public-project"]:focus', 'position:absolute;top:8px;',
    'left:8px;z-index:1;padding:8px 12px;border-radius:4px;',
    'background:var(--project-card);color:var(--project-link);',
    `font:600 14px/1.2 ${FONT};`),
  rules('.public-measure', 'max-width:calc(var(--project-measure) + 2 * ',
    'var(--project-gutter));margin-inline:auto;',
    'padding:24px var(--project-gutter) 48px;font-size:15px;'),
  media('(min-width:40rem)', rule(PAGE, '--project-gutter:24px;'),
    rules('.public-measure', 'padding-top:32px;padding-bottom:64px;'),
    rules('h1', 'font-size:32px;')),
  media('(min-width:56rem)', rule(PAGE, '--project-gutter:32px;')),
  media('(min-width:48rem)', rule(PAGE, '--project-radius-card:26px;')),
  media('(min-width:64rem)', rule(PAGE, '--project-radius-card:28px;')),
];

const BAR = [
  rules('.public-toolbar', 'padding-block:8px 7px;',
    'background:var(--toolbar-ground);',
    'border-bottom:1px solid var(--toolbar-border);'),
  rules('.public-toolbar-inner', 'display:flex;flex-wrap:wrap;',
    'align-items:center;justify-content:space-between;column-gap:8px;',
    'row-gap:8px;min-height:32px;max-width:calc(var(--project-measure) + ',
    '2 * var(--project-gutter));margin-inline:auto;',
    'padding-inline:var(--project-gutter);font-size:15px;'),
  media('(min-width:40rem)',
    rules('.public-toolbar-inner', 'column-gap:16px;')),
  rules('.public-brand', 'display:inline-flex;align-items:center;gap:8px;',
    'min-width:0;'),
  rules('.public-mark', 'display:block;flex-shrink:0;',
    'color:var(--toolbar-mark);'),
  rules('.public-credit', `font:500 13px/20px ${FONT};`,
    'color:var(--toolbar-credit);text-decoration:none;'),
  rules('.public-back', 'display:inline-flex;align-items:center;',
    'min-height:32px;max-width:100%;padding:5px 12px 5px 10px;',
    'border:1.5px solid var(--toolbar-button-border);border-radius:999px;',
    'background:transparent;color:var(--toolbar-button-label);',
    `font:500 14px/20px ${FONT};text-decoration:none;`),
  rules('.public-toolbar a:focus-visible',
    'outline-color:var(--toolbar-focus-ring);'),
  media('(hover:hover)',
    rules('.public-credit:hover', 'color:var(--toolbar-credit-hover);',
      'text-decoration:underline;text-underline-offset:3px;'),
    rules('.public-back:hover',
      'background:var(--toolbar-button-hover-fill);')),
  media('(pointer:coarse)', rules('.public-toolbar-inner', 'min-height:40px;'),
    rules('.public-credit', 'display:inline-block;padding-block:10px;',
      'margin-block:-10px;'),
    rules('.public-back', 'min-height:40px;')),
  media('(width < 30rem)', rules('.public-credit', 'font-size:12px;'),
    rules('.public-back', 'padding-inline:8px 10px;')),
  media('print', rules('.public-toolbar', 'display:none;')),
];

const CHIPS = [
  rules('.project-chips', 'display:flex;flex-wrap:wrap;gap:6px;margin:0;',
    'padding:0;list-style:none;'),
  rules('.project-chips li', 'display:inline-flex;align-items:center;',
    'min-height:24px;padding:2px 10px;border:1px solid transparent;',
    'border-radius:999px;background:var(--project-chip);',
    'color:var(--project-ink);font-size:12px;line-height:18px;',
    'font-weight:500;'),
  rules('.project-chips .project-chips-more', 'color:var(--project-muted);'),
];

const LIST = [
  rules('.project-owner', 'margin-top:4px;color:var(--project-muted);'),
  rules('.project-owner a', 'color:var(--project-muted);'),
  rules('.project-rows', 'margin:24px 0 0;padding:0;list-style:none;',
    'background:var(--project-card);',
    'border:1px solid var(--project-border);',
    'border-radius:var(--project-radius-card);'),
  rules('.project-row', 'display:flex;flex-wrap:wrap;align-items:baseline;',
    'column-gap:12px;row-gap:4px;padding:16px;'),
  rules('.project-row + .project-row',
    'border-top:1px solid var(--project-hairline);'),
  rules('.project-row-name', 'min-width:0;font-size:16px;line-height:24px;',
    'font-weight:600;'),
  rules('.project-row-name a', 'color:var(--project-link);',
    'text-decoration:none;'),
  rules('.project-row-meta', 'flex:1 1 100%;color:var(--project-muted);',
    'font-size:14px;line-height:20px;'),
  rules('.project-row-summary,.project-row .project-chips', 'flex:1 1 100%;'),
  rules('.project-row .project-chips', 'margin-top:4px;'),
  media('(hover:hover)',
    rules('.project-row-name a:hover', 'text-decoration:underline;')),
  media('(min-width:40rem)', rules('.project-row', 'padding:20px 24px;'),
    rules('.project-row-meta', 'flex:0 1 auto;')),
];

const DETAIL = [
  rules('.project-breadcrumb ol', 'display:flex;flex-wrap:wrap;',
    'align-items:center;gap:4px 8px;margin:0 0 12px;padding:0;',
    'list-style:none;color:var(--project-muted);font-size:14px;',
    'line-height:20px;'),
  rules('.project-breadcrumb li + li::before', 'content:"/";',
    'content:"/" / "";margin-inline-end:8px;'),
  rules('.project-breadcrumb a', 'color:var(--project-muted);'),
  rules('.project-lead', 'margin-top:12px;font-size:16px;line-height:1.6;'),
  rules(['.project-columns', '.project-page > .project-facts',
    '.project-page > .project-text'], 'margin-top:24px;'),
  rules('.project-columns', 'display:grid;gap:24px;'),
  rules('.project-facts', 'display:flex;flex-direction:column;gap:20px;',
    'min-width:0;padding:20px;background:var(--project-card);',
    'border:1px solid var(--project-border);',
    'border-radius:var(--project-radius-card);'),
  rules('.project-links', 'display:flex;flex-wrap:wrap;gap:8px;'),
  rules('.project-external', 'display:inline-flex;flex:1 1 10rem;',
    'align-items:center;justify-content:center;gap:8px;min-height:40px;',
    'padding:8px 16px;border:1.5px solid var(--project-ink);',
    'border-radius:999px;background:transparent;',
    `color:var(--project-ink);font:500 14px/20px ${FONT};`,
    'text-align:center;text-decoration:none;'),
  rules('.project-external svg', 'display:block;flex-shrink:0;'),
  rules('.project-external-primary', 'border-color:var(--project-primary);',
    'background:var(--project-primary);color:var(--project-on-primary);'),
  media('(hover:hover)',
    rules('.project-external:hover', 'background:var(--project-hover);'),
    rules('.project-external-primary:hover',
      'border-color:var(--project-primary-hover);',
      'background:var(--project-primary-hover);')),
  rules('.project-facts-list', 'display:flex;flex-direction:column;',
    'gap:12px;margin:0;'),
  rules('.project-facts-list dt,.project-facts h2', 'margin:0;',
    'color:var(--project-muted);font-size:14px;line-height:20px;',
    'font-weight:500;'),
  rules('.project-facts-list dd', 'margin:2px 0 0;line-height:1.5;'),
  rules('.project-facts h2', 'margin-bottom:8px;'),
  rules('.project-text', 'display:flex;flex-direction:column;gap:32px;',
    'min-width:0;'),
  rules('.project-text h2', 'margin-bottom:12px;'),
  rules('.project-outcome', 'padding:20px;background:var(--project-card);',
    'border:1px solid var(--project-border);',
    'border-radius:var(--project-radius-card);'),
  rules('.project-description', 'line-height:1.7;'),
  rules(['.project-description p', '.project-description ul',
    '.project-description ol'], 'margin:0 0 12px;'),
  rules('.project-description ul,.project-description ol',
    'padding-inline-start:24px;'),
  rules('.project-description ul', 'list-style:disc;'),
  rules('.project-description ol', 'list-style:decimal;'),
  rules('.project-description li + li', 'margin-top:4px;'),
  rules('.project-description > :last-child', 'margin-bottom:0;'),
  media('(min-width:56rem)',
    rules('.project-columns', 'grid-template-columns:minmax(0,1fr) 17.5rem;',
      'column-gap:40px;align-items:start;'),
    rules('.project-columns > .project-facts', 'grid-column:2;grid-row:1;'),
    rules('.project-columns > .project-text', 'grid-column:1;grid-row:1;')),
  media('(forced-colors:active)', rules('.public-mark', 'color:CanvasText;'),
    rules('.public-back,.project-external', 'border-color:ButtonBorder;')),
];

const EVIDENCE = [
  rules('.project-evidence-list', 'margin:0;padding:0;list-style:none;',
    'background:var(--project-card);',
    'border:1px solid var(--project-border);',
    'border-radius:var(--project-radius-card);'),
  rules('.project-evidence-item', 'display:flex;flex-direction:column;',
    'gap:4px;padding:16px;'),
  rules('.project-evidence-item + .project-evidence-item',
    'border-top:1px solid var(--project-hairline);'),
  rules('.project-evidence-label,.project-evidence-heading', 'display:flex;',
    'align-items:flex-start;gap:8px;'),
  rules('.project-evidence-label', 'color:var(--project-ink);',
    'font-size:14px;line-height:20px;font-weight:500;'),
  rules('.project-evidence-heading', 'font-size:16px;line-height:24px;',
    'font-weight:600;'),
  rules('.project-evidence-item svg', 'display:block;flex-shrink:0;'),
  rules('.project-evidence-label svg', 'margin-top:2px;'),
  rules('.project-evidence-heading svg', 'margin-top:4px;',
    'color:var(--project-muted);'),
  rules('.project-evidence', 'min-width:0;color:var(--project-link);',
    'text-decoration:none;'),
  rules('.project-evidence-meta', 'padding-inline-start:24px;',
    'color:var(--project-muted);font-size:14px;line-height:20px;'),
  media('(hover:hover)',
    rules('.project-evidence:hover', 'text-decoration:underline;')),
  media('(min-width:40rem)',
    rules('.project-evidence-item', 'padding:20px 24px;')),
  media('(forced-colors:active)',
    rules('.project-evidence-list', 'border-color:CanvasText;')),
];

export const PROJECT_PAGE_STYLE = [
  ...SCHEME, ...FRAME, ...BAR, ...CHIPS, ...LIST, ...DETAIL, ...EVIDENCE,
].join('');
