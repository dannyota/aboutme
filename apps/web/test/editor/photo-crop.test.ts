import { describe, expect, it } from 'vitest';

import {
  cropImageStyle,
  defaultCrop,
  isValidCrop,
  MAX_ZOOM,
  movedBy,
  squareCrop,
  withZoom,
  zoomOf,
} from '../../app/editor/photoCrop';

const portrait = { width: 600, height: 800 };
const landscape = { width: 1200, height: 800 };

function pixelSides(image: { width: number; height: number }, crop: {
  width: number;
  height: number;
}): [number, number] {
  return [
    Math.round(crop.width * image.width),
    Math.round(crop.height * image.height),
  ];
}

describe('photo crop geometry', () => {
  it('starts a portrait on a full-width square over the top third', () => {
    // 3:4 portrait: the 600 px square covers 75% of the height, so centring
    // it on the top third stops at the top edge.
    expect(defaultCrop(portrait)).toEqual({
      x: 0,
      y: 0,
      width: 1,
      height: 0.75,
    });
    // 1:2 portrait: the square is half the height, centred on y = 1/3.
    const tall = { width: 600, height: 1200 };
    const crop = defaultCrop(tall);
    expect(pixelSides(tall, crop)).toEqual([600, 600]);
    expect(crop.y + crop.height / 2).toBeCloseTo(1 / 3, 5);
  });

  it('centres a landscape photo on the largest square', () => {
    const crop = defaultCrop(landscape);
    expect(pixelSides(landscape, crop)).toEqual([800, 800]);
    expect(crop.x).toBeCloseTo(1 / 6, 5);
    expect(crop.y).toBe(0);
  });

  it('zooms around the centre and stays square', () => {
    const start = squareCrop(portrait, 1, 0.5, 0.5);
    const zoomed = withZoom(portrait, start, 2);
    expect(pixelSides(portrait, zoomed)).toEqual([300, 300]);
    expect(zoomed.x + zoomed.width / 2).toBeCloseTo(0.5, 5);
    expect(zoomOf(portrait, zoomed)).toBeCloseTo(2, 5);
    expect(zoomOf(portrait, withZoom(portrait, start, 99))).toBe(MAX_ZOOM);
  });

  it('keeps a moved or zoomed crop inside the image', () => {
    const crop = squareCrop(portrait, 2, 0.9, 0.9);
    expect(isValidCrop(crop)).toBe(true);
    const pushed = movedBy(crop, 1, 1);
    expect(pushed.x + pushed.width).toBeCloseTo(1, 6);
    expect(pushed.y + pushed.height).toBeCloseTo(1, 6);
    const back = movedBy(crop, -5, -5);
    expect([back.x, back.y]).toEqual([0, 0]);
  });

  it('rejects crops outside the image', () => {
    expect(isValidCrop({ x: 0.5, y: 0, width: 0.6, height: 0.5 })).toBe(false);
    expect(isValidCrop({ x: 0, y: 0, width: 0, height: 0.5 })).toBe(false);
    expect(isValidCrop({ x: 0.2, y: 0.06, width: 0.6, height: 0.48 }))
      .toBe(true);
  });

  it('draws a crop by scaling and shifting the image in its frame', () => {
    expect(cropImageStyle({ x: 0.2, y: 0.06, width: 0.6, height: 0.48 }))
      .toEqual({
        position: 'absolute',
        width: '166.666667%',
        height: '208.333333%',
        left: '-33.333333%',
        top: '-12.5%',
        maxWidth: 'none',
        objectFit: 'cover',
      });
  });
});
