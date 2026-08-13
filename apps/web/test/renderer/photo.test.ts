import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import Photo from '../../app/components/resume/primitives/Photo.vue';

describe('authorized resume photo', () => {
  it('uses only the supplied URL and deterministic crop bindings', () => {
    const wrapper = mount(Photo, {
      props: {
        photo: {
          url: 'data:image/png;base64,AA==',
          crop: { x: 0.1, y: 0.2, width: 0.5, height: 0.25 },
        },
      },
    });
    const image = wrapper.get('img');
    expect(image.attributes('src')).toBe('data:image/png;base64,AA==');
    expect(image.attributes('style')).toContain('object-fit: cover');
    expect(image.attributes('style')).toContain(
      'object-position: 20% 26.666666666666668%',
    );
  });

  it('keeps a full-axis crop centered instead of dividing by zero', () => {
    const wrapper = mount(Photo, {
      props: {
        photo: {
          url: 'data:image/png;base64,AA==',
          crop: { x: 0.2, y: 0.3, width: 1, height: 0.5 },
        },
      },
    });
    const image = wrapper.get('img');
    expect(image.attributes('style')).toContain('object-position: 50% 60%');
  });
});
