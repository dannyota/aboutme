// @vitest-environment node

import { afterEach, describe, expect, it, vi } from 'vitest';

const originalZone = process.env.TZ;

afterEach(() => {
  if (originalZone === undefined) delete process.env.TZ;
  else process.env.TZ = originalZone;
  vi.resetModules();
});

async function datesIn(zone: string) {
  process.env.TZ = zone;
  vi.resetModules();
  const { viewsDetailCopy } = await import('../../app/i18n/views');
  return (['en', 'vi'] as const).map((locale) => {
    const copy = viewsDetailCopy[locale];
    return [
      copy.axisDay('2026-09-01'),
      copy.fullDay('2026-09-30'),
      copy.monthName('2026-09'),
    ];
  });
}

describe('views calendar dates', () => {
  it.each(['Asia/Ho_Chi_Minh', 'America/Los_Angeles'])(
    'keep the server calendar day in %s',
    async (zone) => {
      const utc = await datesIn('UTC');
      expect(utc[0]).toEqual(['Sep 1', 'Sep 30, 2026', 'Sep 2026']);
      expect(await datesIn(zone)).toEqual(utc);
    },
  );
});
