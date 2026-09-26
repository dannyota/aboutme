import type { PersonalDetail, Resume } from '@aboutme/schema';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { mount, type VueWrapper } from '@vue/test-utils';
import { nextTick, ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import PublishPreview from
  '../../app/components/editor/PublishPreview.vue';
import PreviewCard from '../../app/components/preview/PreviewCard.vue';
import { contrastRatio } from '../../app/components/resume/clampContrast';
import { acceptedFixture } from './fixture';

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));

afterEach(() => {
  vi.unstubAllGlobals();
  locale.value = 'en';
});

class FakeResizeObserver {
  observe = vi.fn();
  disconnect = vi.fn();
  unobserve = vi.fn();
}

function stubResizeObserver(): void {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
}

function documentWith(overrides: {
  personalDetails?: Partial<Resume['personalDetails']>;
  accent?: string;
}): Resume {
  const base = acceptedFixture().document;
  return {
    ...base,
    personalDetails: { ...base.personalDetails, ...overrides.personalDetails },
    customization: {
      ...base.customization,
      colors: overrides.accent === undefined
        ? base.customization.colors
        : { ...base.customization.colors, accent: overrides.accent },
    },
  };
}

function detail(
  type: PersonalDetail['type'],
  value: string,
  isHidden: boolean,
): PersonalDetail {
  return { id: `detail-${type}`, type, value, isHidden };
}

interface PreviewProps {
  document: Resume;
  slug: string;
  pageTitle: string;
  lng: string;
  photoUrl?: string;
}

function baseProps(overrides: Partial<PreviewProps> = {}): PreviewProps {
  return {
    document: documentWith({ personalDetails: { fullName: 'Ada Lovelace' } }),
    slug: 'ada',
    pageTitle: '',
    lng: 'en',
    ...overrides,
  };
}

async function expandPreview(wrapper: VueWrapper): Promise<void> {
  await wrapper.get('[data-action="toggle-publish-preview"]')
    .trigger('click');
  await nextTick();
  await new Promise((resolve) => setTimeout(resolve, 20));
}

describe('PublishPreview', () => {
  it('is collapsed by default and renders no card until expanded', async () => {
    stubResizeObserver();
    const wrapper = mount(PublishPreview, { props: baseProps() });

    expect(wrapper.findComponent(PreviewCard).exists()).toBe(false);
    expect(
      wrapper.get('[data-action="toggle-publish-preview"]')
        .attributes('aria-expanded'),
    ).toBe('false');

    await expandPreview(wrapper);

    expect(wrapper.findComponent(PreviewCard).exists()).toBe(true);
  });

  it('shows the name and headline on the card and the title, '
    + 'description, and domain in the chat card', async () => {
    stubResizeObserver();
    const wrapper = mount(PublishPreview, {
      props: baseProps({
        document: documentWith({
          personalDetails: {
            fullName: 'Ada Lovelace',
            headline: 'Mathematician',
          },
        }),
      }),
    });
    await expandPreview(wrapper);

    const card = wrapper.findComponent(PreviewCard);
    expect(card.props('card').name).toBe('Ada Lovelace');
    expect(card.props('card').headline).toBe('Mathematician');

    const text = wrapper.get('[data-testid="publish-preview-text"]').text();
    expect(text).toContain('Ada Lovelace');
    expect(text).toContain('Mathematician');
    expect(text).toContain('aboutme.vn');
  });

  it('lets the page-title field override the title', async () => {
    stubResizeObserver();
    const wrapper = mount(PublishPreview, {
      props: baseProps({ pageTitle: 'Ada, Software Engineer' }),
    });
    await expandPreview(wrapper);

    expect(wrapper.get('[data-testid="publish-preview-text"]').text())
      .toContain('Ada, Software Engineer');
  });

  it('shows the photo with both photo and photoUrl set', async () => {
    stubResizeObserver();
    const crop = { x: 0.1, y: 0.2, width: 0.5, height: 0.5 };
    const withPhoto = documentWith({
      personalDetails: {
        fullName: 'Ada Lovelace',
        photo: { key: 'photo-key', crop },
      },
    });

    const both = mount(PublishPreview, {
      props: baseProps({ document: withPhoto, photoUrl: 'blob:photo' }),
    });
    await expandPreview(both);
    expect(both.findComponent(PreviewCard).props('card').photo)
      .toEqual({ url: 'blob:photo', crop });

    const photoOnly = mount(PublishPreview, {
      props: baseProps({ document: withPhoto }),
    });
    await expandPreview(photoOnly);
    expect(photoOnly.findComponent(PreviewCard).props('card').photo)
      .toBeNull();

    const urlOnly = mount(PublishPreview, {
      props: baseProps({ photoUrl: 'blob:photo' }),
    });
    await expandPreview(urlOnly);
    expect(urlOnly.findComponent(PreviewCard).props('card').photo)
      .toBeNull();
  });

  it('clamps a light accent for 3:1 contrast and leaves a dark one '
    + 'unchanged', async () => {
    stubResizeObserver();
    const light = mount(PublishPreview, {
      props: baseProps({
        document: documentWith({
          personalDetails: { fullName: 'Ada Lovelace' },
          accent: '#ffff00',
        }),
      }),
    });
    await expandPreview(light);
    const lightAccent = light.findComponent(PreviewCard).props('card').accent;
    expect(lightAccent).not.toBe('#ffff00');
    expect(contrastRatio(lightAccent, '#ffffff')).toBeGreaterThanOrEqual(3);

    const dark = mount(PublishPreview, {
      props: baseProps({
        document: documentWith({
          personalDetails: { fullName: 'Ada Lovelace' },
          accent: '#1a1a1a',
        }),
      }),
    });
    await expandPreview(dark);
    expect(dark.findComponent(PreviewCard).props('card').accent)
      .toBe('#1a1a1a');
  });

  it('leaks no contact sentinel and falls back to the slug on the card',
    async () => {
      stubResizeObserver();
      const hiddenSentinel = 'ZZ-HIDDEN-9f2b';
      const visibleSentinel = 'ZZ-VISIBLE-7c31';
      const wrapper = mount(PublishPreview, {
        props: baseProps({
          document: documentWith({
            personalDetails: {
              fullName: `Ada ${visibleSentinel} Lovelace`,
              headline: `Works with ${visibleSentinel}`,
              details: [
                detail('location', visibleSentinel, false),
                detail('phone', hiddenSentinel, true),
              ],
            },
          }),
        }),
      });
      await expandPreview(wrapper);

      const text = wrapper.text();
      expect(text).not.toContain(hiddenSentinel);
      expect(text).not.toContain(visibleSentinel);
      expect(wrapper.findComponent(PreviewCard).props('card').name)
        .toBeNull();
      expect(text).toContain('aboutme.vn/ada');
    });

  it('shows the heading copy in English and Vietnamese', () => {
    locale.value = 'en';
    const english = mount(PublishPreview, { props: baseProps() });
    expect(english.text()).toContain('Preview when your resume is shared');

    locale.value = 'vi';
    const vietnamese = mount(PublishPreview, { props: baseProps() });
    expect(vietnamese.text()).toContain('Xem trước khi chia sẻ CV');
  });

  it('marks the scaled card as decorative', async () => {
    stubResizeObserver();
    const wrapper = mount(PublishPreview, { props: baseProps() });
    await expandPreview(wrapper);

    expect(
      wrapper.get('[data-testid="publish-preview-card"]')
        .attributes('aria-hidden'),
    ).toBe('true');
  });
});
