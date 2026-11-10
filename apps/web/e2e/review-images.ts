import type { TestInfo } from '@playwright/test';

export type ReviewImage
  = | 'public-modern-sidebar-vi-dark-390'
    | 'public-executive-band-en-light-1440'
    | 'home-vi-light-390'
    | 'showcase-en-dark-1440'
    | 'library-sheet-en-light-390'
    | 'sign-in-error-vi-390'
    | 'print-main-overflow-p2'
    | 'print-sidebar-overflow-p2';

/** Optional evidence never changes an assertion result. */
export async function captureReviewImage(
  testInfo: TestInfo,
  id: ReviewImage,
  capture: () => Promise<Buffer>,
): Promise<void> {
  const timeout = testInfo.timeout;
  const started = performance.now();
  let expired = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  // Reserve only the bounded capture time, preserving the assertion budget.
  if (timeout > 0) {
    testInfo.setTimeout(timeout + 2_000);
  }
  try {
    await Promise.race([
      (async () => {
        const body = await capture();
        if (!expired) {
          await testInfo.attach(`review-${id}`, {
            body, contentType: 'image/png',
          });
        }
      })(),
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
          expired = true;
          reject(new Error('Evidence unavailable.'));
        }, 1_000);
      }),
    ]);
  } catch {
    testInfo.annotations.push({
      type: 'evidence', description: `review-${id}-unavailable`,
    });
  } finally {
    clearTimeout(timer);
    if (timeout > 0) {
      testInfo.setTimeout(timeout + Math.ceil(performance.now() - started));
    }
  }
}
