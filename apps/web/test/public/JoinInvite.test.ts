import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, type VueWrapper } from '@vue/test-utils';

import JoinInvite from '../../app/components/public/JoinInvite.vue';
import { JOIN_INVITE_STORAGE_KEY } from '../../app/public/joinInvite';

const mounted: VueWrapper[] = [];

function mountInvite(
  props: { lng: 'vi' | 'en'; href: '/register' | '/login' } = {
    lng: 'en',
    href: '/register',
  },
) {
  const root = document.createElement('div');
  document.body.append(root);
  const wrapper = mount(JoinInvite, {
    attachTo: document.body,
    props: { ...props, root },
  });
  mounted.push(wrapper);
  return wrapper;
}

// A real page root: a block <main> that spans the viewport around a centered
// resume box, as in the public page. Both boxes are stubbed since happy-dom
// does no layout.
function mountInviteOnPage(measureRight: number) {
  const root = document.createElement('main');
  root.getBoundingClientRect = () => ({ right: window.innerWidth }) as DOMRect;
  const measure = document.createElement('div');
  measure.className = 'public-measure';
  measure.getBoundingClientRect = () => ({ right: measureRight }) as DOMRect;
  root.append(measure);
  document.body.append(root);
  const wrapper = mount(JoinInvite, {
    attachTo: document.body,
    props: { lng: 'en', href: '/register', root },
  });
  mounted.push(wrapper);
  return wrapper;
}

beforeEach(() => {
  vi.useFakeTimers();
  window.localStorage.clear();
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: 1_440,
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  vi.useRealTimers();
});

describe('JoinInvite', () => {
  it('stays hidden until the 20 s dwell floor', async () => {
    const wrapper = mountInvite();
    expect(wrapper.find('[role="region"]').exists()).toBe(false);
    await vi.advanceTimersByTimeAsync(19_999);
    expect(wrapper.find('[role="region"]').exists()).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[role="region"]').exists()).toBe(true);
  });

  it('never mounts the timing when already closed in this browser', () => {
    window.localStorage.setItem(JOIN_INVITE_STORAGE_KEY, String(Date.now()));
    const wrapper = mountInvite();
    vi.advanceTimersByTime(60_000);
    expect(wrapper.find('[role="region"]').exists()).toBe(false);
  });

  it('labels the region and shows Vietnamese or English copy', async () => {
    const en = mountInvite({ lng: 'en', href: '/register' });
    await vi.advanceTimersByTimeAsync(20_000);
    await en.vm.$nextTick();
    expect(en.get('[role="region"]').attributes('aria-label'))
      .toBe('Create a free resume invite');
    expect(en.text()).toContain('Create a free resume');
    expect(en.get('a').attributes('href')).toBe('/register');

    const vi_ = mountInvite({ lng: 'vi', href: '/login' });
    await vi.advanceTimersByTimeAsync(20_000);
    await vi_.vm.$nextTick();
    expect(vi_.get('[role="region"]').attributes('aria-label'))
      .toBe('Lời mời tạo CV miễn phí');
    expect(vi_.text()).toContain('Tạo CV miễn phí');
    expect(vi_.get('a').attributes('href')).toBe('/login');
  });

  it('closes on the close button and persists it for this browser',
    async () => {
      const wrapper = mountInvite();
      await vi.advanceTimersByTimeAsync(20_000);
      await wrapper.vm.$nextTick();
      await wrapper.get('button').trigger('click');
      expect(wrapper.find('[role="region"]').exists()).toBe(false);
      expect(window.localStorage.getItem(JOIN_INVITE_STORAGE_KEY))
        .not.toBeNull();
    });

  it('closes on Escape', async () => {
    const wrapper = mountInvite();
    await vi.advanceTimersByTimeAsync(20_000);
    await wrapper.vm.$nextTick();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[role="region"]').exists()).toBe(false);
  });

  it('persists a close when the link is followed', async () => {
    const wrapper = mountInvite();
    await vi.advanceTimersByTimeAsync(20_000);
    await wrapper.vm.$nextTick();
    await wrapper.get('a').trigger('click');
    expect(window.localStorage.getItem(JOIN_INVITE_STORAGE_KEY))
      .not.toBeNull();
  });

  it('switches from card to bar placement below the 1024 px floor',
    async () => {
      const wrapper = mountInvite();
      await vi.advanceTimersByTimeAsync(20_000);
      await wrapper.vm.$nextTick();
      expect(wrapper.get('[role="region"]').attributes('data-placement'))
        .toBe('card');

      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        value: 600,
      });
      window.dispatchEvent(new Event('resize'));
      await wrapper.vm.$nextTick();
      expect(wrapper.get('[role="region"]').attributes('data-placement'))
        .toBe('bar');
    });

  it('places the card from the resume box, not the page root', async () => {
    const wrapper = mountInviteOnPage(1_440 - 400);
    await vi.advanceTimersByTimeAsync(20_000);
    await wrapper.vm.$nextTick();
    expect(wrapper.get('[role="region"]').attributes('data-placement'))
      .toBe('card');
  });

  it('places the bar when the resume box leaves under 360 px', async () => {
    const wrapper = mountInviteOnPage(1_440 - 200);
    await vi.advanceTimersByTimeAsync(20_000);
    await wrapper.vm.$nextTick();
    expect(wrapper.get('[role="region"]').attributes('data-placement'))
      .toBe('bar');
  });

  describe('geometry contract', () => {
    async function shown(width: number, measureRight: number) {
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        value: width,
      });
      const wrapper = mountInviteOnPage(measureRight);
      await vi.advanceTimersByTimeAsync(20_000);
      await wrapper.vm.$nextTick();
      return wrapper;
    }

    it('lays out the bar as text, 36 px button, then close', async () => {
      const wrapper = await shown(390, 390);
      const region = wrapper.get('[role="region"]');
      expect(region.attributes('data-placement')).toBe('bar');
      const children = [...region.element.children];
      expect(children.map((child) => child.tagName))
        .toEqual(['P', 'A', 'BUTTON']);
      expect(children[0]!.className).toContain('join-invite-text');
      expect(children[1]!.getAttribute('style')).toContain('height:36px');
      expect(children[2]!.getAttribute('aria-label')).toBe('Close');
    });

    it('drops the bar text below 360 px, keeping button and close',
      async () => {
        const wrapper = await shown(359, 359);
        const region = wrapper.get('[role="region"]');
        expect(region.find('.join-invite-text').exists()).toBe(false);
        expect([...region.element.children].map((child) => child.tagName))
          .toEqual(['A', 'BUTTON']);
      });

    it('pads the page by the bar height only while the bar shows',
      async () => {
        const set = vi.spyOn(document.body.style, 'setProperty');
        const remove = vi.spyOn(document.body.style, 'removeProperty');
        const wrapper = await shown(390, 390);
        expect(set).toHaveBeenCalledWith(
          'padding-bottom',
          'calc(56px + env(safe-area-inset-bottom))',
        );
        await wrapper.get('button').trigger('click');
        await wrapper.vm.$nextTick();
        expect(remove).toHaveBeenCalledWith('padding-bottom');
      });

    it('never pads the page for the card', async () => {
      const set = vi.spyOn(document.body.style, 'setProperty');
      const wrapper = await shown(1_920, 1_920 - 500);
      expect(wrapper.get('[role="region"]').attributes('data-placement'))
        .toBe('card');
      expect(set).not.toHaveBeenCalledWith(
        'padding-bottom',
        expect.anything(),
      );
    });

    it('lays out the card as close row, text, then a 40 px button',
      async () => {
        const wrapper = await shown(1_920, 1_920 - 500);
        const children = [...wrapper.get('[role="region"]').element.children];
        expect(children.map((child) => child.tagName))
          .toEqual(['DIV', 'P', 'A']);
        expect(children[0]!.querySelector('button')!.getAttribute('style'))
          .toContain('width:32px');
        expect(children[2]!.getAttribute('style')).toContain('height:40px');
      });

    it('slides in with the Web Animations API unless motion is reduced',
      async () => {
        const animate = vi.fn();
        const originalMatchMedia = window.matchMedia;
        Object.defineProperty(HTMLElement.prototype, 'animate', {
          configurable: true,
          value: animate,
        });
        try {
          window.matchMedia = ((query: string) => ({
            matches: false,
            media: query,
          })) as typeof window.matchMedia;
          await shown(390, 390);
          expect(animate).toHaveBeenCalledTimes(1);
          expect(animate.mock.calls[0]![1]).toMatchObject({ duration: 200 });

          animate.mockClear();
          window.matchMedia = ((query: string) => ({
            matches: true,
            media: query,
          })) as typeof window.matchMedia;
          await shown(390, 390);
          expect(animate).not.toHaveBeenCalled();
        } finally {
          window.matchMedia = originalMatchMedia;
          Reflect.deleteProperty(HTMLElement.prototype, 'animate');
        }
      });
  });
});
