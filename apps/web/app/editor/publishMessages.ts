import type { PublishCopy } from '../i18n/publish';
import type { PublishControllerState } from './publishController';

// Maps controller outcomes to the dialog's catalog text. A code the catalog
// does not know gets the generic text, never the code itself.

export function issueMessage(copy: PublishCopy, code: string): string {
  switch (code) {
    case 'required_for_live':
    case 'requires_live':
    case 'invalid_format':
    case 'reserved':
    case 'required':
    case 'visible_entry_required':
      return copy.issue[code];
    default:
      return copy.invalid;
  }
}

export function blockedMessage(
  copy: PublishCopy,
  reason: Extract<PublishControllerState, { kind: 'blocked' }>['reason'],
): string {
  return copy.blocked[reason];
}

export function failureMessage(copy: PublishCopy, code: string): string {
  switch (code) {
    case 'provider_disabled':
    case 'provider_unavailable':
    case 'csrf_rejected':
    case 'save_failed':
      return copy.failed[code];
    default:
      return copy.failed.generic;
  }
}
