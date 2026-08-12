import {
  ALLOWED_ATTRIBUTES,
  ALLOWED_TAGS,
  ALLOWED_URL_SCHEMES,
  EXTERNAL_REL,
  FORBIDDEN_ATTRIBUTE_PREFIXES,
  FORBIDDEN_TAGS,
} from '@aboutme/schema/sanitizer';
import type { Config, DOMPurify } from 'dompurify';

const purifier: DOMPurify | undefined
  = import.meta.client && typeof window !== 'undefined'
    ? (await import('dompurify')).default
    : undefined;

const allowedSchemes = new Set(ALLOWED_URL_SCHEMES);
const allowedAttributeNames = [
  ...new Set(Object.values(ALLOWED_ATTRIBUTES).flat()),
];

const config: Config = {
  ALLOWED_TAGS: [...ALLOWED_TAGS],
  ALLOWED_ATTR: allowedAttributeNames,
  FORBID_TAGS: [...FORBIDDEN_TAGS],
  ALLOW_ARIA_ATTR: false,
  ALLOW_DATA_ATTR: false,
  ALLOW_UNKNOWN_PROTOCOLS: false,
  SANITIZE_NAMED_PROPS: true,
};

const explicitAllowedScheme = (value: string): boolean => {
  const browserValue = [...value]
    .filter((character) => character.charCodeAt(0) > 0x20)
    .join('');
  const match = /^([a-z][a-z0-9+.-]*):/i.exec(browserValue);
  const scheme = match?.[1]?.toLowerCase();
  return scheme !== undefined && allowedSchemes.has(scheme);
};

let hooksInstalled = false;

const installHooks = (): void => {
  if (hooksInstalled || purifier === undefined) return;

  purifier.addHook('uponSanitizeAttribute', (element, attribute) => {
    const tag = element.tagName.toLowerCase();
    const name = attribute.attrName.toLowerCase();
    if (
      FORBIDDEN_ATTRIBUTE_PREFIXES.some((prefix) =>
        name.startsWith(prefix.toLowerCase()),
      )
    ) {
      attribute.keepAttr = false;
      return;
    }
    if (!(ALLOWED_ATTRIBUTES[tag] ?? []).includes(name)) {
      attribute.keepAttr = false;
      return;
    }
    if (name === 'href' && !explicitAllowedScheme(attribute.attrValue)) {
      attribute.keepAttr = false;
      return;
    }
    if (name === 'target' && attribute.attrValue !== '_blank') {
      attribute.keepAttr = false;
      return;
    }
    if (name === 'rel') {
      attribute.attrValue = EXTERNAL_REL;
    }
  });

  purifier.addHook('afterSanitizeAttributes', (element) => {
    if (element.tagName.toLowerCase() !== 'a') return;

    element.setAttribute('rel', EXTERNAL_REL);
    if (element.getAttribute('target') !== '_blank') {
      element.removeAttribute('target');
    }
  });

  hooksInstalled = true;
};

export const sanitizeRichText = (html: string): string => {
  if (purifier === undefined) return html;

  installHooks();
  return purifier.sanitize(html, config);
};
