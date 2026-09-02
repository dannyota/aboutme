import { mount } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { describe, expect, it } from 'vitest';

import EditorPreview from '../../app/components/editor/EditorPreview.vue';
import { acceptedFixture } from './fixture';

describe('EditorPreview', () => {
  it('passes only the optimistic document and paged render context', () => {
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const renderer = wrapper.getComponent({ name: 'ResumeDocument' });
    expect(renderer.props('document')).toStrictEqual(accepted.document);
    expect(renderer.props('context')).toEqual({ lng: 'en', mode: 'paged' });
  });

  it('renders without the photo while the read is pending', () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = {
      key: 'resumes/resume-1/private-object.jpg',
    };
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: 'en',
        photoRead: { kind: 'loading', binding: 'k', generation: 1 },
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const renderer = wrapper.getComponent({ name: 'ResumeDocument' });
    expect(renderer.props('document').personalDetails.photo).toBeUndefined();
    expect(renderer.props('context')).toEqual({ lng: 'en', mode: 'paged' });
    expect(wrapper.html()).not.toContain('private-object.jpg');
  });

  it('names the unavailable state and the photo panel', () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = { key: 'resumes/resume-1/p.jpg' };
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: 'en',
        photoRead: {
          kind: 'suspended',
          binding: 'k',
          generation: 1,
          reason: 'read-failed',
        },
      },
      global: { stubs: { ResumeDocument: true } },
    });

    expect(wrapper.findComponent({ name: 'ResumeDocument' }).exists()).toBe(
      true,
    );
    expect(wrapper.html()).not.toContain('resumes/resume-1/p.jpg');
  });

  it(
    'keeps the safe render notice when the fallback renderer fails',
    async () => {
      const accepted = acceptedFixture();
      accepted.document.personalDetails.photo = {
        key: 'resumes/resume-1/private-object.jpg',
      };
      let projectedPhoto: unknown = 'not-observed';
      const wrapper = mount(EditorPreview, {
        props: {
          document: accepted.document,
          lng: 'en',
          photoRead: { kind: 'loading', binding: 'k', generation: 1 },
        },
        global: {
          stubs: {
            ResumeDocument: defineComponent({
              name: 'ResumeDocument',
              props: { document: { type: Object, required: true } },
              setup(props) {
                projectedPhoto = (
                  props.document as {
                    personalDetails: { photo?: unknown };
                  }
                ).personalDetails.photo;
                throw new Error('renderer failed');
              },
              template: '<div />',
            }),
          },
        },
      });
      await nextTick();

      expect(projectedPhoto).toBeUndefined();
      expect(wrapper.get('[role="status"]').text()).toContain(
        'Preview is temporarily unavailable. Your edits are still safe.',
      );
      expect(wrapper.html()).not.toContain('private-object.jpg');
    },
  );

  it('passes an authorized data URL without exposing the stored key', () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = {
      key: 'resumes/resume-1/private-object.jpg',
    };
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: 'en',
        photoUrl: 'data:image/jpeg;base64,AA==',
      },
      global: { stubs: { ResumeDocument: true } },
    });

    expect(
      wrapper.getComponent({ name: 'ResumeDocument' }).props('context'),
    ).toEqual({
      lng: 'en',
      mode: 'paged',
      photoUrl: 'data:image/jpeg;base64,AA==',
    });
    expect(wrapper.html()).not.toContain('private-object.jpg');
  });
});
