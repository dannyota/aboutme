import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import Photo from '../../app/components/resume/primitives/Photo.vue';

// The frame shows exactly the crop rectangle, scaled to fill it. The image box
// maps the whole image onto frame coordinates and object-fit: cover keeps the
// image aspect, so a pixel-square crop is exact and any other crop is never
// stretched or letterboxed.

const mountPhoto = (crop?: {
  x: number;
  y: number;
  width: number;
  height: number;
}) => mount(Photo, {
  props: {
    photo: {
      url: 'data:image/png;base64,AA==',
      ...(crop === undefined ? {} : { crop }),
    },
  },
});

const styleOf = (style: string | undefined): Record<string, string> =>
  Object.fromEntries((style ?? '')
    .split(';')
    .map((declaration) => declaration.split(':').map((part) => part.trim()))
    .filter(([name]) => name)
    .map(([name, value]) => [name!, value!]));

describe('authorized resume photo', () => {
  it('frames the image and uses only the supplied URL', () => {
    const wrapper = mountPhoto();
    expect(wrapper.element.tagName).toBe('SPAN');
    expect(wrapper.classes()).toContain('resume-photo');
    const image = wrapper.get('img');
    expect(image.attributes('src')).toBe('data:image/png;base64,AA==');
    expect(image.classes()).toContain('resume-photo-image');
    expect(image.attributes('alt')).toBe('');
  });

  it('fills the frame with the centred cover when there is no crop', () => {
    expect(styleOf(mountPhoto().get('img').attributes('style'))).toEqual({
      left: '0%',
      top: '0%',
      width: '100%',
      height: '100%',
    });
  });

  it('scales and offsets the image so the crop fills the frame', () => {
    // A 1024x1280 photo cropped to its 614.4px square at (204.8, 76.8).
    const crop = { x: 0.2, y: 0.06, width: 0.6, height: 0.48 };
    const style = styleOf(mountPhoto(crop).get('img').attributes('style'));
    expect(style).toEqual({
      width: '166.666667%',
      height: '208.333333%',
      left: '-33.333333%',
      top: '-12.5%',
    });
  });

  it('keeps a full-image crop inside the frame', () => {
    expect(styleOf(mountPhoto({ x: 0, y: 0, width: 1, height: 1 })
      .get('img').attributes('style'))).toEqual({
      width: '100%',
      height: '100%',
      left: '0%',
      top: '0%',
    });
  });

  it('never emits url(), a backslash, or object-position', () => {
    const crop = { x: 0.1, y: 0.2, width: 0.5, height: 0.25 };
    const html = mountPhoto(crop).html();
    expect(html).not.toMatch(/url\(|\\|object-position/u);
  });
});
