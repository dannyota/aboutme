import type { Customization, Resume, Section } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';
import { mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { applyTemplate } from '../../app/components/resume/applyTemplate';
import type {
  ResolvedRenderModel,
} from '../../app/components/resume/resolveRenderModel';
import { resolveRenderModel } from
  '../../app/components/resume/resolveRenderModel';
import ResumeHeader from '../../app/components/resume/ResumeHeader.vue';
import ProjectSection from
  '../../app/components/resume/sections/ProjectSection.vue';

// The header photo sits on top, left, or right of the name block
// (docs/adr/0044-header-photo-position-and-project-subtitle.md).

type Position = ResolvedRenderModel['header']['photoPosition'];

const photo = { url: 'data:image/png;base64,AA==' };

function mountHeader(position: Position, withPhoto = true) {
  return mount(ResumeHeader, {
    props: {
      personalDetails: {
        fullName: 'Ada Lovelace',
        headline: 'Engineer',
        details: [],
      },
      header: {
        align: 'center',
        detailsLayout: 'inline',
        iconStyle: 'outline',
        photoPosition: position,
      },
      ...(withPhoto ? { photo } : {}),
    },
  });
}

describe('header photo position', () => {
  it.each(['left', 'right'] as const)(
    'marks a %s photo and wraps the text beside it',
    (position) => {
      const header = mountHeader(position);
      expect(header.attributes('data-photo-position')).toBe(position);
      const children = [...header.element.children];
      expect(children.map((child) => child.className)).toEqual([
        'resume-photo',
        'resume-header-text',
      ]);
      const text = header.get('.resume-header-text');
      expect(text.find('.resume-name').text()).toBe('Ada Lovelace');
      expect(text.find('.resume-headline').text()).toBe('Engineer');
      // header.align applies inside the text column.
      expect(header.attributes('style')).toContain('text-align: center');
    },
  );

  it('keeps the top layout unmarked, and ignores a side choice without a photo',
    () => {
      expect(mountHeader('top').attributes('data-photo-position'))
        .toBeUndefined();
      const noPhoto = mountHeader('left', false);
      expect(noPhoto.attributes('data-photo-position')).toBeUndefined();
      expect(noPhoto.find('.resume-photo').exists()).toBe(false);
      expect(noPhoto.find('.resume-header-text').exists()).toBe(true);
    });

  it('resolves an absent position as top', () => {
    const document = fixture();
    delete document.personalDetails.photo;
    delete document.customization.header;
    expect(resolveRenderModel(document, { lng: 'en', mode: 'continuous' })
      .header.photoPosition).toBe('top');
    document.customization.header = {
      align: 'left',
      detailsLayout: 'inline',
      iconStyle: 'outline',
      photoPosition: 'left',
    };
    expect(resolveRenderModel(document, { lng: 'en', mode: 'paged' })
      .header.photoPosition).toBe('left');
  });
});

describe('template switches keep the photo position', () => {
  function withPosition(
    customization: Customization,
    photoPosition?: Position,
  ): Customization {
    const { header, ...rest } = customization;
    if (photoPosition === undefined) {
      if (header === undefined) return rest;
      const { photoPosition: _stored, ...kept } = header;
      return { ...rest, header: kept };
    }
    return {
      ...rest,
      header: {
        ...(header ?? {
          align: 'left',
          detailsLayout: 'inline',
          iconStyle: 'outline',
        }),
        photoPosition,
      },
    };
  }

  it.each(TEMPLATES.map((preset) => [preset.id, preset] as const))(
    'carries left over and keeps absence absent for %s',
    (_id, preset) => {
      const document = fixture();
      const left = applyTemplate(
        withPosition(document.customization, 'left'),
        preset,
        document.content,
      );
      expect(left.header?.photoPosition).toBe('left');
      // The preset's own header settings still apply.
      if (preset.customization.header !== undefined) {
        expect(left.header?.align).toBe(preset.customization.header.align);
      }

      const none = applyTemplate(
        withPosition(document.customization),
        preset,
        document.content,
      );
      expect(none.header?.photoPosition).toBeUndefined();
      expect(none.header === undefined)
        .toBe(preset.customization.header === undefined);
    },
  );
});

describe('project subtitle', () => {
  it('renders a subtitle between the title and the dates', () => {
    const section: Extract<Section, { sectionType: 'project' }> = {
      sectionType: 'project',
      entries: [{
        id: '00000000-0000-4000-8000-000000000001',
        isHidden: false,
        title: 'aboutme',
        subtitle: 'Open-source resume builder',
        link: 'https://aboutme.vn',
      }],
    };
    const wrapper = mount(ProjectSection, {
      props: { section, dateFormat: 'MM/YYYY' },
    });
    const header = wrapper.get('.entry-header');
    expect(header.get('.entry-title').text()).toBe('aboutme');
    expect(header.get('.entry-subtitle').text())
      .toBe('Open-source resume builder');
    expect(header.get('.entry-subtitle').find('a').exists()).toBe(false);

    const bare = mount(ProjectSection, {
      props: {
        section: {
          ...section,
          entries: [{ ...section.entries[0]!, subtitle: undefined }],
        },
        dateFormat: 'MM/YYYY',
      },
    });
    expect(bare.find('.entry-subtitle').exists()).toBe(false);
  });
});

function fixture(): Resume {
  return JSON.parse(readFileSync(resolve(
    process.cwd(),
    '../../packages/schema/fixtures/full.json',
  ), 'utf8')) as Resume;
}
