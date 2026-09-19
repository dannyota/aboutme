import type { PersonalDetail } from '@aboutme/schema';

/**
 * The link a contact detail renders as, or null for plain text. The renderer
 * re-checks every value itself and never trusts write-time validation
 * (ADR 0013, ADR 0041, ADR 0043).
 */

const URL_TYPES = new Set<PersonalDetail['type']>([
  'website',
  'linkedin',
  'github',
  'twitter',
  'custom',
]);

const EMAIL_MAX = 254;
// Unusual in real addresses; ? # % would also let the value add mail headers
// or a fragment to the mailto: URL.
const FORBIDDEN_EMAIL = /[\p{White_Space}\p{Cc}\p{Cf}<>"'`()\\,;:?#%]/u;
const PHONE_SEPARATORS = /[ .()-]/gu;
const PHONE = /^\+?[0-9]{3,20}$/u;

/**
 * At most 254 characters, exactly one @ with something before it, and a
 * domain that contains a dot but neither starts nor ends with one.
 */
export function emailHref(value: string): string | null {
  const length = [...value].length;
  if (length === 0 || length > EMAIL_MAX || FORBIDDEN_EMAIL.test(value)) {
    return null;
  }
  const at = value.indexOf('@');
  const domain = value.slice(at + 1);
  const valid = at > 0
    && !domain.includes('@')
    && domain.includes('.')
    && !domain.startsWith('.')
    && !domain.endsWith('.');
  return valid ? `mailto:${value}` : null;
}

/** "(+84) 374837720" links as tel:+84374837720. */
export function phoneHref(value: string): string | null {
  const dialled = value.replace(PHONE_SEPARATORS, '');
  return PHONE.test(dialled) ? `tel:${dialled}` : null;
}

export function contactHref(detail: PersonalDetail): string | null {
  switch (detail.type) {
    case 'email':
      return emailHref(detail.value);
    case 'phone':
      return phoneHref(detail.value);
    case 'location':
      return null;
    default:
      return URL_TYPES.has(detail.type) && detail.value.startsWith('https://')
        ? detail.value
        : null;
  }
}
