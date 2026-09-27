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

beforeEach(() => {
  vi.useFakeTimers();
  window.localStorage.clear();
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: 1_440,
  });
});

afterEach(() => {
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
});
