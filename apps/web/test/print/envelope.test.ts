// @vitest-environment node

import { describe, expect, it } from 'vitest';

import {
  decodePrintEnvelope,
  PRINT_ENVELOPE_MAX_BYTES,
  PRINT_FAILURE,
  PRINT_PHOTO_MAX_BYTES,
} from '../../server/utils/print/envelope';
import { printEnvelope, RESUME_ID } from './fixture';

const decode = (value: unknown) => decodePrintEnvelope(JSON.stringify(value));
const rejected = (source: string): void => {
  expect(() => decodePrintEnvelope(source)).toThrow(PRINT_FAILURE);
};

describe('print envelope', () => {
  it('accepts only the exact six-field current envelope', () => {
    expect(decode(printEnvelope())).toEqual(printEnvelope());
    rejected(`${JSON.stringify(printEnvelope()).slice(0, -1)},"extra":true}`);
    rejected(JSON.stringify({ ...printEnvelope(), version: 2 }));
    rejected(JSON.stringify({
      ...printEnvelope(),
      document: { ...printEnvelope().document, accountId: RESUME_ID },
    }));
    const missingName = printEnvelope();
    delete (missingName.document.personalDetails as { fullName?: string })
      .fullName;
    rejected(JSON.stringify(missingName));
  });

  it('rejects duplicate keys at any depth', () => {
    const source = JSON.stringify(printEnvelope());
    rejected(`${source.slice(0, -1)},"lng":"en"}`);
    rejected(source.replace(
      '"personalDetails":{',
      '"personalDetails":{"fullName":"duplicate",',
    ));
  });

  it(
    'requires canonical identifiers, revision, generation, and language',
    () => {
      const changes: Record<string, unknown>[] = [
        { resumeId: '00000000-0000-0000-0000-000000000000' },
        { resumeId: RESUME_ID.toUpperCase() },
        { resumeId: 'not-a-uuid' },
        { revision: '0' },
        { revision: '01' },
        { revision: '9223372036854775808' },
        { publicGeneration: '6' },
        { publicGeneration: 7 },
        { lng: 'en-us' },
        { lng: 'iw' },
        { lng: 'x'.repeat(36) },
        { lng: 'not_a_tag' },
      ];
      for (const change of changes) {
        rejected(JSON.stringify({ ...printEnvelope(), ...change }));
      }
      expect(decode({ ...printEnvelope(), publicGeneration: '7', lng: 'und' }))
        .toMatchObject({ publicGeneration: '7', lng: 'und' });
      expect(decode({ ...printEnvelope(), lng: 'x-private' })).toMatchObject({
        lng: 'x-private',
      });
    },
  );

  it('accepts only canonical inline JPEG and PNG photo data', () => {
    const photo = (url: string) => ({
      ...printEnvelope(),
      document: {
        ...printEnvelope().document,
        personalDetails: {
          ...printEnvelope().document.personalDetails,
          photo: { url },
        },
      },
    });
    expect(decode(photo('data:image/png;base64,AA=='))).toBeTruthy();
    expect(decode(photo('data:image/jpeg;base64,/9j/'))).toBeTruthy();
    for (const url of [
      'https://resume.example/photo.png',
      'data:image/gif;base64,AA==',
      'data:image/png;base64,AA',
      'data:image/png;base64,AA===',
      'data:image/png;base64,A A=',
      'data:image/png;base64,',
    ]) rejected(JSON.stringify(photo(url)));
  });

  it('accepts explicitly cleared optional links in an owner draft', () => {
    const envelope = printEnvelope();
    const section = 'a6a0a5fa-7fe4-4d52-be40-0da2db95de12';
    envelope.document.content = {
      [section]: {
        sectionType: 'custom',
        entries: [{
          id: '55b412f2-fd0d-4c41-be7d-f2603d6058b3',
          title: 'Hackathon Winner',
          titleLink: '',
        }],
      },
    };
    envelope.document.customization.layout.sections.main = [section];
    expect(decode(envelope)).toEqual(envelope);

    const entry = envelope.document.content[section]!.entries[0]!;
    entry.titleLink = 'javascript:alert(1)';
    rejected(JSON.stringify(envelope));
  });

  it('accepts the photo byte cap and rejects one decoded byte more', () => {
    const photo = (bytes: number) => {
      const encoded = Buffer.alloc(bytes).toString('base64');
      const url = `data:image/png;base64,${encoded}`;
      const envelope = printEnvelope();
      envelope.document.personalDetails.photo = { url };
      return JSON.stringify(envelope);
    };
    expect(decodePrintEnvelope(photo(PRINT_PHOTO_MAX_BYTES))).toBeTruthy();
    rejected(photo(PRINT_PHOTO_MAX_BYTES + 1));
  });

  it('accepts the JSON byte cap and rejects one UTF-8 byte more', () => {
    const source = JSON.stringify(printEnvelope());
    expect(decodePrintEnvelope(
      source + ' '.repeat(PRINT_ENVELOPE_MAX_BYTES - source.length),
    )).toBeTruthy();
    rejected(source + ' '.repeat(PRINT_ENVELOPE_MAX_BYTES - source.length + 1));
  });
});
