// @vitest-environment jsdom

import {
  ALLOWED_ATTRIBUTES,
  ALLOWED_TAGS,
  ALLOWED_URL_SCHEMES,
  EXTERNAL_REL,
  FORBIDDEN_ATTRIBUTE_PREFIXES,
  HOSTILE_CORPUS,
} from '@aboutme/schema/sanitizer';
import { describe, expect, it } from 'vitest';
import { sanitizeRichText } from '../../app/utils/sanitizeRichText';

const HTML_NAMESPACE = 'http://www.w3.org/1999/xhtml';
const ARBITRARY_STRING_SEED = 0x51a17e3d;
const ARBITRARY_STRING_CASES = 512;

const allowedTags = new Set<string>(ALLOWED_TAGS);
const allowedSchemes = new Set<string>(ALLOWED_URL_SCHEMES);
const expectedRelTokens = EXTERNAL_REL.split(/\s+/);

type Rule = 'attr' | 'href' | 'ns' | 'rel' | 'tag' | 'target';

interface NeutralizationViolation {
  readonly detail: string;
  readonly rule: Rule;
}

function parseRichText(html: string): Document {
  return new DOMParser().parseFromString(html, 'text/html');
}

function trimC0ControlsAndSpace(value: string): string {
  let start = 0;
  let end = value.length;

  while (start < end && value.charCodeAt(start) <= 0x20) {
    start += 1;
  }
  while (end > start && value.charCodeAt(end - 1) <= 0x20) {
    end -= 1;
  }

  return value.slice(start, end);
}

function hasAllowedExplicitScheme(href: string): boolean {
  // Match the URL parser's removal of tabs/newlines and outer C0 controls
  // before checking that the source supplied an explicit admitted scheme.
  const normalized = trimC0ControlsAndSpace(href.replace(/[\t\n\r]/g, ''));
  const match = /^([a-z][a-z\d+.-]*):/i.exec(normalized);

  return match !== null && allowedSchemes.has(match[1].toLowerCase());
}

function hasExactExternalRel(rel: string | null): boolean {
  if (rel === null) {
    return false;
  }

  const actual = rel
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((token) => token.toLowerCase());

  const hasExpectedCount = actual.length === expectedRelTokens.length;
  const hasDistinctTokens = new Set(actual).size === expectedRelTokens.length;
  const hasExpectedTokens = expectedRelTokens.every((token) =>
    actual.includes(token),
  );

  return hasExpectedCount && hasDistinctTokens && hasExpectedTokens;
}

function neutralizationViolations(html: string): NeutralizationViolation[] {
  const document = parseRichText(html);
  const violations: NeutralizationViolation[] = [];

  for (const scaffold of [
    document.documentElement,
    document.head,
    document.body,
  ]) {
    for (const attribute of Array.from(scaffold.attributes)) {
      violations.push({
        detail: `${scaffold.localName} retained ${attribute.name}`,
        rule: 'attr',
      });
    }
  }

  function visit(node: Node): void {
    if (node.nodeType === Node.ELEMENT_NODE) {
      const element = node as Element;
      const tag = element.localName.toLowerCase();

      if (element.namespaceURI !== HTML_NAMESPACE) {
        violations.push({
          detail: `${tag} namespace ${element.namespaceURI ?? 'null'}`,
          rule: 'ns',
        });
      }
      if (!allowedTags.has(tag)) {
        violations.push({
          detail: `<${tag}> is outside the allowlist`,
          rule: 'tag',
        });
      }

      const tagAttributes = new Set<string>(ALLOWED_ATTRIBUTES[tag] ?? []);
      for (const attribute of Array.from(element.attributes)) {
        const name = attribute.name.toLowerCase();
        const hasForbiddenPrefix = FORBIDDEN_ATTRIBUTE_PREFIXES.some((prefix) =>
          name.startsWith(prefix.toLowerCase()),
        );

        if (hasForbiddenPrefix || !tagAttributes.has(name)) {
          violations.push({
            detail: `<${tag}> retained disallowed attribute ${attribute.name}`,
            rule: 'attr',
          });
        }
      }

      if (tag === 'a') {
        const href = element.getAttribute('href');
        if (href !== null && !hasAllowedExplicitScheme(href)) {
          violations.push({
            detail: `<a> retained disallowed href: ${href}`,
            rule: 'href',
          });
        }
        if (!hasExactExternalRel(element.getAttribute('rel'))) {
          violations.push({
            detail: `<a> did not receive the exact hardened rel token set`,
            rule: 'rel',
          });
        }

        const target = element.getAttribute('target');
        if (target !== null && target !== '_blank') {
          violations.push({
            detail: `<a> retained non-_blank target ${target}`,
            rule: 'target',
          });
        }
      }

      if (tag === 'template' && element.namespaceURI === HTML_NAMESPACE) {
        visit((element as HTMLTemplateElement).content);
      }
    }

    for (const child of Array.from(node.childNodes)) {
      visit(child);
    }
  }

  for (const root of [
    ...document.head.childNodes,
    ...document.body.childNodes,
  ]) {
    visit(root);
  }

  return violations;
}

function isNeutralized(html: string): boolean {
  return neutralizationViolations(html).length === 0;
}

function anchorsIn(html: string): HTMLAnchorElement[] {
  return Array.from(parseRichText(html).querySelectorAll('a'));
}

const negativeControls: readonly {
  readonly html: string;
  readonly id: string;
  readonly rule: Rule;
}[] = [
  { html: '<script>alert(1)</script>', id: 'forbidden-tag', rule: 'tag' },
  {
    html: '<p style="display:none">hidden</p>',
    id: 'smuggled-attribute',
    rule: 'attr',
  },
  {
    html: '<a href="javascript:alert(1)" rel="noopener noreferrer">click</a>',
    id: 'forbidden-href',
    rule: 'href',
  },
  {
    html: '<a href="https://example.com" rel="opener">click</a>',
    id: 'forged-rel',
    rule: 'rel',
  },
  {
    html: '<a href="https://example.com" rel="noopener noreferrer" target="_self">click</a>',
    id: 'forged-target',
    rule: 'target',
  },
  {
    html: '<svg><a href="https://example.com" rel="noopener noreferrer">click</a></svg>',
    id: 'foreign-namespace',
    rule: 'ns',
  },
];

interface AdversarialCase {
  readonly category: string;
  readonly expectedHref?: string | null;
  readonly expectedTarget?: '_blank' | null;
  readonly html: string;
  readonly id: string;
}

function joinHtml(...parts: readonly string[]): string {
  return parts.join('');
}

const adversarialCases: readonly AdversarialCase[] = [
  {
    category: 'mutation',
    html: joinHtml(
      '<noscript><p title="</noscript>',
      '<img src=x onerror=alert(1)>">safe</p></noscript>',
    ),
    id: 'noscript-parser-pivot',
  },
  {
    category: 'mutation',
    html: joinHtml(
      '<template><p><svg><style><img src=x onerror=alert(1)>',
      '</style></svg></p></template><p>safe</p>',
    ),
    id: 'template-foreign-content-pivot',
  },
  {
    category: 'mutation',
    html: joinHtml(
      '<math><mtext><table><mglyph><style><!--</style>',
      '<img title="--><img src=x onerror=alert(1)>">',
      '</mglyph></table></mtext></math>',
    ),
    id: 'math-mglyph-mutation-pivot',
  },
  {
    category: 'foreign-content',
    html: joinHtml(
      '<svg><foreignObject><math><mi>',
      '<a xlink:href="javascript:alert(1)">click</a>',
      '</mi></math></foreignObject></svg>',
    ),
    id: 'svg-math-xlink-pivot',
  },
  {
    category: 'scheme-obfuscation',
    expectedHref: null,
    html: '<a href="javascript%3Aalert(1)">click</a>',
    id: 'url-encoded-colon',
  },
  {
    category: 'scheme-obfuscation',
    expectedHref: null,
    html: '<a href="JaVa&#x53;CrIpT&colon;alert(1)">click</a>',
    id: 'mixed-entity-and-case',
  },
  {
    category: 'scheme-obfuscation',
    expectedHref: null,
    html: '<a href="java&#13;script&#58;alert(1)">click</a>',
    id: 'entity-carriage-return',
  },
  {
    category: 'attribute-smuggling',
    expectedHref: 'https://example.com/profile',
    expectedTarget: '_blank',
    html: joinHtml(
      '<a href="https://example.com/profile" target="_blank" rel="opener" ',
      'formaction="javascript:alert(1)" ',
      'srcdoc="<script>alert(1)</script>" ',
      'xlink:href="javascript:alert(1)" ',
      'style="background:url(javascript:alert(1))">click</a>',
    ),
    id: 'anchor-multi-attribute-smuggling',
  },
  {
    category: 'attribute-smuggling',
    html: joinHtml(
      '<p style="background:url(javascript:alert(1))" ',
      'onanimationstart="alert(1)" ',
      'srcdoc="<img src=x onerror=alert(1)>">safe</p>',
    ),
    id: 'plain-element-attribute-smuggling',
  },
  {
    category: 'namespace-confusion',
    html: joinHtml(
      '<math><a href="javascript:alert(1)" ',
      'rel="noopener noreferrer">click</a></math>',
    ),
    id: 'math-anchor-confusion',
  },
  {
    category: 'namespace-confusion',
    html: joinHtml(
      '<svg><a xlink:href="javascript:alert(1)" ',
      'target="_blank" rel="opener">click</a></svg>',
    ),
    id: 'svg-anchor-confusion',
  },
  {
    category: 'rel-target-forgery',
    expectedHref: 'https://example.com',
    expectedTarget: '_blank',
    html: joinHtml(
      '<a href="https://example.com" target="_blank" ',
      'rel="noopener opener noreferrer external">click</a>',
    ),
    id: 'extra-opener-rel-token',
  },
  {
    category: 'rel-target-forgery',
    expectedHref: 'https://example.com',
    expectedTarget: null,
    html: joinHtml(
      '<a href="https://example.com" target="_self" ',
      'rel="noopener noreferrer">click</a>',
    ),
    id: 'non-blank-target',
  },
  {
    category: 'rel-target-forgery',
    expectedHref: 'mailto:test@example.com',
    expectedTarget: null,
    html: joinHtml(
      '<a href="mailto:test@example.com" ',
      'target="_blank " rel="noreferrer">mail</a>',
    ),
    id: 'near-blank-target',
  },
];

type RandomSource = () => number;

function seededRandom(seed: number): RandomSource {
  let state = seed;

  return () => {
    state = (state * 1_664_525 + 1_013_904_223) % 0x1_0000_0000;
    return state / 0x1_0000_0000;
  };
}

function randomInteger(random: RandomSource, upperExclusive: number): number {
  return Math.floor(random() * upperExclusive);
}

const stringFragments = [
  '<',
  '>',
  '&',
  '"',
  String.fromCharCode(39),
  '=',
  '/',
  '\\',
  'javascript:',
  '&#x3a;',
  '</template>',
  '<svg/onload=alert(1)>',
  '\u0000',
  '\r\n\t',
] as const;

function arbitraryString(random: RandomSource): string {
  const fragmentCount = randomInteger(random, 96);
  let result = '';

  for (let index = 0; index < fragmentCount; index += 1) {
    if (randomInteger(random, 4) === 0) {
      result += stringFragments[randomInteger(random, stringFragments.length)];
    } else {
      result += String.fromCharCode(randomInteger(random, 0x1_0000));
    }
  }

  return result;
}

describe('independent rich-text neutralization predicate', () => {
  it.each(negativeControls)(
    'rejects live negative control $id',
    ({ html, rule }) => {
      const violations = neutralizationViolations(html);

      expect(isNeutralized(html)).toBe(false);
      expect(violations.map((violation) => violation.rule)).toContain(rule);
    },
  );

  it('rejects every structurally hostile raw corpus payload', () => {
    const structurallyHostile = HOSTILE_CORPUS.filter(
      ({ id }) => id !== 'js-scheme-bare',
    );

    expect(structurallyHostile).toHaveLength(HOSTILE_CORPUS.length - 1);
    for (const { id, payload } of structurallyHostile) {
      expect(neutralizationViolations(payload), id).not.toEqual([]);
    }
  });

  it('accepts dangerous-looking bare text as an inert text node', () => {
    const bareText = 'javascript:alert(1)';
    const document = parseRichText(bareText);

    expect(isNeutralized(bareText)).toBe(true);
    expect(document.body.childElementCount).toBe(0);
    expect(document.body.textContent).toBe(bareText);
  });

  it('accepts the complete safe tag and link surface', () => {
    const safe = joinHtml(
      '<p>text<br><strong>strong</strong><em>em</em><u>u</u></p>',
      '<ol><li>one</li></ol><ul><li>two</li></ul>',
      '<a href="tel:+84123456789" target="_blank" ',
      'rel="noopener noreferrer">call</a>',
    );

    expect(neutralizationViolations(safe)).toEqual([]);
  });
});

describe('client sanitizer adversarial cases', () => {
  it.each(HOSTILE_CORPUS)(
    'neutralizes corpus payload $id',
    ({ id, payload }) => {
      const output = sanitizeRichText(payload);

      expect(neutralizationViolations(output), id).toEqual([]);
      expect(sanitizeRichText(output), id).toBe(output);
    },
  );

  it.each(adversarialCases)(
    'neutralizes $category payload $id',
    ({ expectedHref, expectedTarget, html, id }) => {
      const output = sanitizeRichText(html);
      const anchors = anchorsIn(output);

      expect(neutralizationViolations(output), id).toEqual([]);
      expect(sanitizeRichText(output), id).toBe(output);
      for (const anchor of anchors) {
        expect(anchor.getAttribute('rel'), id).toBe(EXTERNAL_REL);
      }

      if (expectedHref !== undefined) {
        expect(anchors, id).toHaveLength(1);
        expect(anchors[0].getAttribute('href'), id).toBe(expectedHref);
      }
      if (expectedTarget !== undefined) {
        expect(anchors, id).toHaveLength(1);
        expect(anchors[0].getAttribute('target'), id).toBe(expectedTarget);
      }
    },
  );

  it('neutralizes seeded arbitrary strings and is idempotent', () => {
    const random = seededRandom(ARBITRARY_STRING_SEED);

    for (let index = 0; index < ARBITRARY_STRING_CASES; index += 1) {
      const input = arbitraryString(random);
      const output = sanitizeRichText(input);
      const caseLabel = `seed=${ARBITRARY_STRING_SEED} case=${index}`;

      expect(neutralizationViolations(output), caseLabel).toEqual([]);
      expect(sanitizeRichText(output), caseLabel).toBe(output);
    }
  });
});
