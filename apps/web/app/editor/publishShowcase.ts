import { computed, type ComputedRef, ref, type Ref, watch } from 'vue';

import {
  isShowcaseRole,
  type OwnerShowcase,
  type PublishShowcaseFields,
} from '../lib/showcaseContract';
import type { ServerValidationIssue } from './attempt';

/** Issues on these fields show at the showcase block, not in the list. */
export function isShowcaseIssuePath(path: string): boolean {
  return path === 'showcaseEnabled' || path === 'showcaseRole';
}

export type ShowcaseBlockReason = 'public' | 'open-view';

export interface PublishShowcaseInputs {
  readonly live: Ref<boolean>;
  readonly signInToView: Ref<boolean>;
  readonly busy: ComputedRef<boolean>;
  readonly issues: ComputedRef<readonly ServerValidationIssue[]>;
}

/**
 * The publish dialog's community showcase state (docs/design/ui/
 * landing-and-library.md, Publish dialog block). `enabled` is the owner's own
 * switch and survives changes to Public resume and sign in to view, so
 * undoing such a change shows the stored value again; `on` is what the
 * switch shows. A request carries a showcase field only when the owner
 * changed it, and never `showcaseEnabled: true` while the switch is disabled.
 */
export function usePublishShowcase(inputs: PublishShowcaseInputs) {
  const stored = ref<OwnerShowcase | null>(null);
  const enabled = ref(false);
  const role = ref('');
  const dismissed = ref(false);

  const reason = computed<ShowcaseBlockReason | null>(() => {
    if (!inputs.live.value) return 'public';
    return inputs.signInToView.value ? 'open-view' : null;
  });
  const available = computed(() => reason.value === null);
  const on = computed(() => enabled.value && available.value);
  const storedRole = computed(() => {
    const value = stored.value?.role ?? null;
    return isShowcaseRole(value) ? value : '';
  });
  // The Listed line shows only while it describes what the owner sees.
  const listed = computed(
    () => stored.value !== null && on.value && !inputs.busy.value,
  );
  // The server's showcase issue for the last request, until the owner
  // changes a showcase control.
  const issueCode = computed<string | null>(() => {
    if (dismissed.value) return null;
    const issue = inputs.issues.value.find(
      (candidate) => isShowcaseIssuePath(candidate.path),
    );
    return issue === undefined ? null : issue.code;
  });

  watch(inputs.issues, () => {
    dismissed.value = false;
  });

  function sync(metadata: { readonly showcase?: OwnerShowcase | null }): void {
    stored.value = metadata.showcase ?? null;
    enabled.value = stored.value !== null;
    role.value = storedRole.value;
    dismissed.value = false;
  }

  function setEnabled(value: boolean): void {
    enabled.value = value;
    dismissed.value = true;
  }

  function setRole(value: string): void {
    role.value = isShowcaseRole(value) ? value : '';
    dismissed.value = true;
  }

  function fields(): PublishShowcaseFields {
    const changedSwitch = enabled.value !== (stored.value !== null);
    return {
      ...(changedSwitch && (!enabled.value || available.value)
        ? { showcaseEnabled: enabled.value }
        : {}),
      ...(on.value && role.value !== storedRole.value
        ? { showcaseRole: role.value }
        : {}),
    };
  }

  return {
    on,
    role,
    reason,
    listed,
    issueCode,
    sync,
    setEnabled,
    setRole,
    fields,
  };
}
