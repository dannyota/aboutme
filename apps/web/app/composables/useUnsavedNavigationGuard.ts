import { onBeforeUnmount, onMounted, type Ref } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';

import type { ResumeRecord } from '../stores/resumes';

export function hasUnsafeWork(record: ResumeRecord | undefined): boolean {
  return (
    record !== undefined
    && (record.pending.length > 0
      || record.attempt !== null
      || record.conflicts.length > 0
      || Object.keys(record.issues).length > 0
      || record.templateState?.kind === 'partial'
      || record.completeReadRequired
      || record.sessionLost
      || record.opaquePhotoOutcome !== null)
  );
}

/** Session loss may redirect only when no work must stay in RAM. */
export function shouldRetainEditorOnSessionLoss(
  record: ResumeRecord | undefined,
): boolean {
  return hasUnsafeWork(record);
}

export function useUnsavedNavigationGuard(
  record: Readonly<Ref<ResumeRecord | undefined>>,
): void {
  const beforeUnload = (event: BeforeUnloadEvent): void => {
    if (!hasUnsafeWork(record.value)) return;
    event.preventDefault();
    event.returnValue = '';
  };

  onBeforeRouteLeave(() => !hasUnsafeWork(record.value));
  onMounted(() => window.addEventListener('beforeunload', beforeUnload));
  onBeforeUnmount(() =>
    window.removeEventListener('beforeunload', beforeUnload),
  );
}
