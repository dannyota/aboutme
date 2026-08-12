import {
  ALLOWED_ATTRIBUTES,
  ALLOWED_TAGS,
  ALLOWED_URL_SCHEMES,
  EXTERNAL_REL,
  FORBIDDEN_ATTRIBUTE_PREFIXES,
} from '@aboutme/schema/sanitizer';

export interface NeutralizationViolation {
  kind: string;
  value: string;
}

const allowedTags = new Set(ALLOWED_TAGS);
const allowedSchemes = new Set(ALLOWED_URL_SCHEMES);

const explicitScheme = (value: string): string | null => {
  const browserValue = [...value]
    .filter((character) => character.charCodeAt(0) > 0x20)
    .join('');
  const match = /^([a-z][a-z0-9+.-]*):/i.exec(browserValue);
  return match?.[1]?.toLowerCase() ?? null;
};

export const neutralizationViolations = (
  html: string,
): NeutralizationViolation[] => {
  const document = new DOMParser().parseFromString(html, 'text/html');
  const fragment = document.createElement('template');
  fragment.innerHTML = html;
  const violations: NeutralizationViolation[] = [];

  for (const element of fragment.content.querySelectorAll('*')) {
    const tag = element.tagName.toLowerCase();
    if (!allowedTags.has(tag)) {
      violations.push({ kind: 'tag', value: tag });
    }

    const allowedAttributes = new Set(ALLOWED_ATTRIBUTES[tag] ?? []);
    for (const attribute of element.attributes) {
      const name = attribute.name.toLowerCase();
      if (!allowedAttributes.has(name)) {
        violations.push({ kind: 'attribute', value: `${tag}.${name}` });
      }
      if (
        FORBIDDEN_ATTRIBUTE_PREFIXES.some((prefix) =>
          name.startsWith(prefix.toLowerCase()),
        )
      ) {
        violations.push({ kind: 'attribute-prefix', value: name });
      }
    }

    if (tag !== 'a') continue;

    const href = element.getAttribute('href');
    if (href !== null) {
      const scheme = explicitScheme(href);
      if (scheme === null || !allowedSchemes.has(scheme)) {
        violations.push({ kind: 'href', value: href });
      }
    }
    if (element.getAttribute('rel') !== EXTERNAL_REL) {
      violations.push({
        kind: 'rel',
        value: element.getAttribute('rel') ?? '<missing>',
      });
    }
    const target = element.getAttribute('target');
    if (target !== null && target !== '_blank') {
      violations.push({ kind: 'target', value: target });
    }
  }

  return violations;
};

export const expectNeutralized = (html: string): void => {
  const violations = neutralizationViolations(html);
  if (violations.length > 0) {
    throw new Error(`unsafe rich text: ${JSON.stringify(violations)}`);
  }
};
