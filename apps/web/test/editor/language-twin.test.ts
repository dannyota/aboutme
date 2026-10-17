import type { PersonalDetail, Resume } from '@aboutme/schema';
import { afterEach, describe, expect, it } from 'vitest';

import { parseCurrentDocument } from '../../app/editor/documentValidation';
import {
  dismissLanguageTwin,
  forgetLanguageTwin,
  languageTwin,
  readLanguageTwin,
  rememberLanguageTwin,
  twinTarget,
} from '../../app/editor/languageTwin';
import { acceptedFixture } from './fixture';

const PHONE = '123e4567-e89b-12d3-a456-426614174001';
const BIRTH = '123e4567-e89b-12d3-a456-426614174002';
const ZALO = '123e4567-e89b-12d3-a456-426614174003';
const NEW_ZALO = '123e4567-e89b-12d3-a456-426614174004';

afterEach(() => {
  forgetLanguageTwin();
});

function detail(
  id: string,
  type: PersonalDetail['type'],
  value: string,
  label?: string,
): PersonalDetail {
  return {
    id,
    type,
    value,
    isHidden: false,
    ...(label === undefined ? {} : { label }),
  };
}

function resume(): Resume {
  const document = structuredClone(acceptedFixture().document);
  document.personalDetails = {
    fullName: 'An Nguyen',
    photo: {
      key: 'resumes/resume-1/photo-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg',
    },
    details: [
      detail(PHONE, 'phone', '+84 90 000 0001'),
      detail(BIRTH, 'custom', '1998', 'Ngày sinh'),
      detail(ZALO, 'custom', 'cùng số', 'Zalo'),
    ],
  };
  document.content = {
    work: {
      sectionType: 'work',
      displayName: 'Kinh nghiệm làm việc',
      entries: [],
    },
    extra: {
      sectionType: 'custom',
      displayName: 'My journey',
      entries: [],
    },
  };
  document.customization.dateFormat = 'MM/YYYY';
  document.customization.layout.sections = {
    main: ['work', 'extra'],
    sidebar: [],
  };
  return document;
}

describe('languageTwin', () => {
  it('copies a Vietnamese resume into an unpublished English draft', () => {
    const document = resume();
    const before = structuredClone(document);
    const twin = languageTwin({
      title: 'Backend',
      lng: 'vi',
      document,
      newId: () => NEW_ZALO,
    });

    expect(twin).not.toBeNull();
    expect(document).toEqual(before);
    expect(twin?.lng).toBe('en');
    expect(twin?.title).toBe('Backend · English');
    expect(twin?.document.customization.dateFormat).toBe('Mon YYYY');
    expect(twin?.document.personalDetails.photo).toBeUndefined();
    expect(twin?.document.personalDetails.details?.map((item) => item.id))
      .toEqual([PHONE]);
    expect(twin?.document.content.work?.displayName).toBe('Experience');
    expect(twin?.document.content.extra?.displayName).toBe('My journey');
    expect(twin?.notes).toEqual([
      'photo-removed',
      'birth-removed',
      'zalo-removed',
      'headings-left',
    ]);
    expect(parseCurrentDocument(twin?.document)).toEqual(twin?.document);
  });

  it('copies an English resume into Vietnamese and adds Zalo', () => {
    const document = resume();
    document.personalDetails.details = [
      detail(PHONE, 'phone', '+84 90 000 0001'),
    ];
    document.content = {
      work: {
        sectionType: 'work',
        displayName: 'Experience',
        entries: [],
      },
    };
    document.customization.dateFormat = 'Mon YYYY';
    document.customization.layout.sections = {
      main: ['work'],
      sidebar: [],
    };

    const twin = languageTwin({
      title: 'Backend · English',
      lng: 'en',
      document,
      newId: () => NEW_ZALO,
    });

    expect(twin?.lng).toBe('vi');
    expect(twin?.title).toBe('Backend · Tiếng Việt');
    expect(twin?.document.customization.dateFormat).toBe('MM/YYYY');
    expect(twin?.document.content.work?.displayName)
      .toBe('Kinh nghiệm làm việc');
    expect(twin?.document.personalDetails.details).toEqual([
      detail(PHONE, 'phone', '+84 90 000 0001'),
      detail(NEW_ZALO, 'custom', 'cùng số', 'Zalo'),
    ]);
    expect(twin?.notes).toEqual(['photo-removed', 'zalo-added']);
    expect(parseCurrentDocument(twin?.document)).toEqual(twin?.document);
  });

  it('leaves a full contact list unchanged instead of adding Zalo', () => {
    const document = resume();
    document.personalDetails.photo = undefined;
    document.personalDetails.details = Array.from(
      { length: 16 },
      (_, index) => detail(
        `123e4567-e89b-12d3-a456-4266141740${index.toString(16)}`,
        index === 0 ? 'phone' : 'custom',
        index === 0 ? '+84 90 000 0001' : `item ${index}`,
        index === 0 ? undefined : `Label ${index}`,
      ),
    );

    const twin = languageTwin({
      title: 'Backend',
      lng: 'en',
      document,
      newId: () => NEW_ZALO,
    });

    expect(twin?.document.personalDetails.details).toHaveLength(16);
    expect(twin?.notes).toContain('zalo-full');
    expect(twin?.notes).not.toContain('zalo-added');
  });

  it('refuses a resume whose language is not Vietnamese or English', () => {
    expect(twinTarget('ja')).toBeNull();
    expect(twinTarget('en-US')).toBeNull();
    expect(languageTwin({
      title: 'Backend',
      lng: 'ja',
      document: resume(),
      newId: () => NEW_ZALO,
    })).toBeNull();
  });

  it('keeps the title inside 160 characters', () => {
    const twin = languageTwin({
      title: 'á'.repeat(200),
      lng: 'vi',
      document: resume(),
      newId: () => NEW_ZALO,
    });
    expect(Array.from(twin?.title ?? '')).toHaveLength(160);
    expect(twin?.title.endsWith(' · English')).toBe(true);
  });

  it('shows the checklist only for the resume that was just created', () => {
    const twin = languageTwin({
      title: 'Backend',
      lng: 'vi',
      document: resume(),
      newId: () => NEW_ZALO,
    });
    expect(twin).not.toBeNull();
    if (twin === null) return;
    rememberLanguageTwin('created', twin);
    expect(readLanguageTwin('created')?.target).toBe('en');
    expect(readLanguageTwin('other')).toBeNull();
    dismissLanguageTwin('created');
    expect(readLanguageTwin('created')).toBeNull();
  });
});
