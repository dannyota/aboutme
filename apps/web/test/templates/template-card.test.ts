import { describe, expect, it } from 'vitest';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';

import TemplateCard from '../../app/components/templates/TemplateCard.vue';
import { galleryCopy } from '../../app/i18n/templates';
import { galleryTemplate } from '../../app/templates/catalog';

// A gallery card with a stored sample page image shows that image instead of
// the live scaled render; one without a sample, or without `pageImageAlt`
// (the homepage's cards), still shows the live render (DESIGN.md, Library).

const atsPlain = galleryTemplate('ats-plain')!;
const classicSerif = galleryTemplate('classic-serif')!;

describe('TemplateCard', () => {
  it('shows the stored page image for a template with a sample', async () => {
    const copy = galleryCopy.en;
    const wrapper = await mountSuspended(TemplateCard, {
      props: {
        illustrative: copy.illustrative,
        locale: 'en',
        pageImageAlt: copy.pageImageAlt,
        template: atsPlain,
      },
    });
    const image = wrapper.get('img[data-page-image]');
    expect(image.attributes('src'))
      .toBe('/templates/pages/ats-plain-en-p1.png');
    expect(image.attributes('width')).toBe('1240');
    expect(image.attributes('height')).toBe('1754');
    expect(image.attributes('alt')).toBe(
      'Page 1 of the ATS Plain sample resume',
    );
    expect(image.attributes('loading')).toBe('lazy');
    expect(wrapper.find('[data-sheet-thumbnail]').exists()).toBe(false);
  });

  it('follows the site locale for the alt text', async () => {
    const copy = galleryCopy.vi;
    const wrapper = await mountSuspended(TemplateCard, {
      props: {
        illustrative: copy.illustrative,
        locale: 'vi',
        pageImageAlt: copy.pageImageAlt,
        template: atsPlain,
      },
    });
    const image = wrapper.get('img[data-page-image]');
    expect(image.attributes('alt')).toBe('Trang 1 của CV mẫu ATS Plain');
  });

  it('loads the page image eagerly when told to', async () => {
    const copy = galleryCopy.en;
    const wrapper = await mountSuspended(TemplateCard, {
      props: {
        eager: true,
        illustrative: copy.illustrative,
        locale: 'en',
        pageImageAlt: copy.pageImageAlt,
        template: atsPlain,
      },
    });
    const image = wrapper.get('img[data-page-image]');
    expect(image.attributes('loading')).toBe('eager');
  });

  it('shows the live render for a template without a sample', async () => {
    const copy = galleryCopy.en;
    const wrapper = await mountSuspended(TemplateCard, {
      props: {
        illustrative: copy.illustrative,
        locale: 'en',
        pageImageAlt: copy.pageImageAlt,
        template: classicSerif,
      },
    });
    await flushPromises();
    expect(wrapper.find('[data-sheet-thumbnail]').exists()).toBe(true);
    expect(wrapper.find('img[data-page-image]').exists()).toBe(false);
  });

  it(
    'shows the live render for a sample template without pageImageAlt',
    async () => {
      const copy = galleryCopy.en;
      const wrapper = await mountSuspended(TemplateCard, {
        props: {
          illustrative: copy.illustrative,
          locale: 'en',
          template: atsPlain,
        },
      });
      await flushPromises();
      expect(wrapper.find('[data-sheet-thumbnail]').exists()).toBe(true);
      expect(wrapper.find('img[data-page-image]').exists()).toBe(false);
    },
  );
});
