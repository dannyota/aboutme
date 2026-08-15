import { shallowMount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import EditorPreview from '../../app/components/editor/EditorPreview.vue';
import { acceptedFixture } from './fixture';

describe('EditorPreview', () => {
  it('passes only the optimistic document and paged render context', () => {
    const accepted = acceptedFixture();
    const wrapper = shallowMount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
    });

    const renderer = wrapper.getComponent({ name: 'ResumeDocument' });
    expect(renderer.props('document')).toStrictEqual(accepted.document);
    expect(renderer.props('context')).toEqual({ lng: 'en', mode: 'paged' });
    expect(wrapper.get('[data-estimated-pages-label]').text()).toBe(
      'Estimated pages',
    );
  });

  it('suspends the complete preview while an accepted photo is unavailable',
    () => {
      const accepted = acceptedFixture();
      accepted.document.personalDetails.photo = {
        key: 'resumes/resume-1/photo.jpg',
      };
      const wrapper = shallowMount(EditorPreview, {
        props: { document: accepted.document, lng: 'en' },
      });

      expect(wrapper.findComponent({ name: 'ResumeDocument' }).exists())
        .toBe(false);
      expect(wrapper.get('[role="status"]').text()).toContain(
        'Preview is waiting for the authorized photo',
      );
    });

  it('passes an authorized data URL without exposing the stored key', () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = {
      key: 'resumes/resume-1/private-object.jpg',
    };
    const wrapper = shallowMount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: 'en',
        photoUrl: 'data:image/jpeg;base64,AA==',
      },
    });

    expect(wrapper.getComponent({ name: 'ResumeDocument' }).props('context'))
      .toEqual({
        lng: 'en',
        mode: 'paged',
        photoUrl: 'data:image/jpeg;base64,AA==',
      });
    expect(wrapper.html()).not.toContain('private-object.jpg');
  });
});
