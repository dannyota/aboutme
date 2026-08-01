import { describe, expect, it } from 'vitest';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import PlaceholderHero from '../app/components/PlaceholderHero.vue';

describe('PlaceholderHero', () => {
  it('renders the given title and tagline into the DOM', async () => {
    const wrapper = await mountSuspended(PlaceholderHero, {
      props: {
        title: 'aboutme test title',
        tagline: 'aboutme test tagline',
      },
    });

    expect(wrapper.find('h1').text()).toBe('aboutme test title');
    expect(wrapper.find('p').text()).toBe('aboutme test tagline');
  });

  it('falls back to the documented defaults with no props', async () => {
    const wrapper = await mountSuspended(PlaceholderHero);

    expect(wrapper.get('[data-testid="placeholder-hero"]').text()).toContain(
      'aboutme',
    );
    expect(wrapper.find('h1').text()).toBe('aboutme');
  });
});
