import { existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { SAMPLES } from '@aboutme/schema/samples';

import manifest from '../../public/templates/pages/manifest.json';
import { galleryCopy } from '../../app/i18n/templates';
import {
  samplePageImage,
  samplePageImages,
} from '../../app/templates/pageImages';

// Page-one images for gallery samples, stored by sample-pages.spec and read
// build-time so the server-rendered card carries the image and its
// dimensions (DESIGN.md, Library).

describe('samplePageImage', () => {
  it('returns page one of every sample, matching the manifest', () => {
    for (const { templateId, lng } of SAMPLES) {
      const image = samplePageImage(templateId, lng);
      expect(image).toBeDefined();
      const entry = manifest.find((candidate) =>
        candidate.templateId === templateId && candidate.lng === lng)!;
      expect(image!.src).toBe(`/templates/pages/${entry.files[0]}`);
      expect(image!.src.startsWith('/templates/pages/')).toBe(true);
      expect(image!.width).toBe(entry.width);
      expect(image!.height).toBe(entry.height);
      expect(image!.width).toBeGreaterThan(0);
      expect(image!.height).toBeGreaterThan(0);
      const publicPath = resolve(
        __dirname,
        '../../public/templates/pages',
        entry.files[0]!,
      );
      expect(existsSync(publicPath)).toBe(true);
    }
  });

  it('has no image for a template without a sample', () => {
    expect(samplePageImage('classic-serif', 'en')).toBeUndefined();
    expect(samplePageImage('classic-serif', 'vi')).toBeUndefined();
  });
});

describe('samplePageImages', () => {
  it('returns every stored page of each sample in order', () => {
    for (const entry of manifest) {
      const images = samplePageImages(
        entry.templateId,
        entry.lng as 'en' | 'vi',
      );
      expect(images).toHaveLength(entry.pages);
      expect(images.map((image) => image.number))
        .toEqual(entry.files.map((_, index) => index + 1));
      images.forEach((image, index) => {
        expect(image.src).toBe(`/templates/pages/${entry.files[index]}`);
        expect(image.width).toBe(entry.width);
        expect(image.height).toBe(entry.height);
      });
    }
  });

  it('is empty for a template without a sample', () => {
    expect(samplePageImages('classic-serif', 'en')).toEqual([]);
    expect(samplePageImages('classic-serif', 'vi')).toEqual([]);
  });
});

describe('gallery page image alt text', () => {
  it('names the template in both site languages', () => {
    expect(galleryCopy.en.pageImageAlt('ATS Plain')).toBe(
      'Page 1 of the ATS Plain sample resume',
    );
    expect(galleryCopy.vi.pageImageAlt('ATS Plain')).toBe(
      'Trang 1 của CV mẫu ATS Plain',
    );
  });
});
