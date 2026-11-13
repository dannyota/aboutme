import type { PersonalDetail } from '@aboutme/schema';

const URL_TYPES = new Set<PersonalDetail['type']>([
  'website',
  'linkedin',
  'github',
  'twitter',
  'custom',
]);

const EMAIL_MAX = 254;
const FORBIDDEN_EMAIL = /[\p{White_Space}\p{Cc}\p{Cf}<>"'`()\\,;:?#%]/u;
const PHONE_SEPARATORS = /[ .()-]/gu;
const PHONE = /^\+?[0-9]{3,20}$/u;

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
