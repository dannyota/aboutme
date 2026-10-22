import type { PersonalDetail, Resume } from '@aboutme/schema';
import { mount, flushPromises } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

import LanguageTwinReview
  from '../../app/components/editor/LanguageTwinReview.vue';
import { setSiteLocale } from '../support/locale';

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
vi.mock('../../app/composables/agentGrants', () => ({
  useAgentGrants: () => ({
    grants: { value: [] },
    refresh: async () => {},
  }),
}));

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

  it('copies into Vietnamese and suggests optional Zalo', () => {
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
    });

    expect(twin?.lng).toBe('vi');
    expect(twin?.title).toBe('Backend · Tiếng Việt');
    expect(twin?.document.customization.dateFormat).toBe('MM/YYYY');
    expect(twin?.document.content.work?.displayName)
      .toBe('Kinh nghiệm làm việc');
    expect(twin?.document.personalDetails.details).toEqual([
      detail(PHONE, 'phone', '+84 90 000 0001'),
    ]);
    expect(twin?.notes).toEqual(['photo-removed', 'zalo-suggested']);
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
    });

    expect(twin?.document.personalDetails.details).toHaveLength(16);
    expect(twin?.notes).toContain('zalo-suggested');
    expect(twin?.document.personalDetails.details)
      .toEqual(document.personalDetails.details);
  });

  it.each([
    { details: [] },
    { details: [detail(PHONE, 'phone', '+84 90 000 0001')] },
  ])(
    'preserves contacts and suggests Zalo regardless of phone presence',
    ({ details }) => {
      const document = resume();
      document.personalDetails.details = details;
      const twin = languageTwin({ title: 'Backend', lng: 'en', document });
      expect(twin?.document.personalDetails.details).toEqual(details);
      expect(twin?.notes).toContain('zalo-suggested');
    },
  );

  it('refuses a resume whose language is not Vietnamese or English', () => {
    expect(twinTarget('ja')).toBeNull();
    expect(twinTarget('en-US')).toBeNull();
    expect(languageTwin({
      title: 'Backend',
      lng: 'ja',
      document: resume(),
    })).toBeNull();
  });

  it('keeps the title inside 160 characters', () => {
    const twin = languageTwin({
      title: 'á'.repeat(200),
      lng: 'vi',
      document: resume(),
    });
    expect(Array.from(twin?.title ?? '')).toHaveLength(160);
    expect(twin?.title.endsWith(' · English')).toBe(true);
  });

  it('shows the checklist only for the resume that was just created', () => {
    const twin = languageTwin({
      title: 'Backend',
      lng: 'vi',
      document: resume(),
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

describe('LanguageTwinReview', () => {
  it.each([
    ['en', 'This resume was created as an unpublished copy.',
      'Add Zalo if you use it and want recruiters to contact you there.',
      'Done reviewing'],
    ['vi', 'CV này được tạo dưới dạng bản sao chưa công khai.',
      'Thêm Zalo nếu bạn dùng và muốn nhà tuyển dụng liên hệ qua đó.',
      'Đã xem xong'],
  ] as const)(
    'describes creation in the past and dismisses the %s checklist',
    async (locale, created, zalo, dismiss) => {
      setSiteLocale(locale);
      const twin = languageTwin({
        title: 'Backend', lng: 'en', document: resume(),
      })!;
      rememberLanguageTwin('created', twin);
      const wrapper = mount(LanguageTwinReview, {
        props: { resumeId: 'created', title: twin.title },
        global: { stubs: { NuxtLink: true } },
      });
      await flushPromises();
      expect(wrapper.text()).toContain(created);
      expect(wrapper.text()).toContain(zalo);
      expect(wrapper.text()).not.toContain('This copy is not public');
      expect(wrapper.text()).not.toContain('Bản này chưa công khai');
      const button = wrapper.findAll('button')
        .find((item) => item.text() === dismiss)!;
      await button.trigger('click');
      expect(wrapper.find('[data-testid="language-twin-review"]').exists())
        .toBe(false);
      expect(readLanguageTwin('created')).toBeNull();
      wrapper.unmount();
      const reopened = mount(LanguageTwinReview, {
        props: { resumeId: 'created', title: twin.title },
      });
      await flushPromises();
      expect(reopened.find('[data-testid="language-twin-review"]').exists())
        .toBe(false);
      reopened.unmount();
    },
  );
});
