import { onBeforeUnmount, onMounted, type Ref } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';

import type { ResumeRecord } from '../stores/resumes';
import type { FieldDrafts } from './useFieldDrafts';

export function hasUnsafeWork(record: ResumeRecord | undefined): boolean {
  return (
    record !== undefined
    && (record.pending.length > 0
      || record.attempt !== null
      || record.conflicts.length > 0
      || Object.keys(record.issues).length > 0
      || record.templateState?.kind === 'partial'
      || (record.completeReadRequired && !record.discarded)
      || record.sessionLost
      || record.opaquePhotoOutcome !== null)
  );
}

export function shouldRetainEditorOnSessionLoss(
  record: ResumeRecord | undefined,
): boolean {
  return hasUnsafeWork(record);
}

export function flushAndCheckUnsaved(
  record: ResumeRecord | undefined,
  drafts: FieldDrafts | undefined,
): boolean {
  const draftsRemain = drafts?.flushAll() ?? false;
  return draftsRemain || hasUnsafeWork(record);
}

export function useUnsavedNavigationGuard(
  record: Readonly<Ref<ResumeRecord | undefined>>,
  drafts?: FieldDrafts,
): void {
  useLeaveGuard(() => flushAndCheckUnsaved(record.value, drafts));
}

export function useLeaveGuard(
  unsafe: () => boolean,
  confirmLeave?: () => Promise<boolean>,
): void {
  const beforeUnload = (event: BeforeUnloadEvent): void => {
    if (!unsafe()) return;
    event.preventDefault();
    event.returnValue = '';
  };

  onBeforeRouteLeave(() => !unsafe() || (confirmLeave?.() ?? false));
  onMounted(() => window.addEventListener('beforeunload', beforeUnload));
  onBeforeUnmount(() =>
    window.removeEventListener('beforeunload', beforeUnload),
  );
}
