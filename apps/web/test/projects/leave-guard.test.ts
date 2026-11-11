import { flushPromises, mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import { createMemoryHistory, createRouter, RouterView } from 'vue-router';
import { describe, expect, it, vi } from 'vitest';

import {
  useLeaveGuard,
} from '../../app/composables/useUnsavedNavigationGuard';

async function setup(
  unsafe: () => boolean,
  confirmLeave?: () => Promise<boolean>,
) {
  const Editor = defineComponent({
    setup() {
      useLeaveGuard(unsafe, confirmLeave);
      return () => h('p', 'editor');
    },
  });
  const Other = defineComponent({ render: () => h('p', 'other') });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/editor', component: Editor },
      { path: '/other', component: Other },
    ],
  });
  await router.push('/editor');
  await router.isReady();
  const wrapper = mount(defineComponent({ render: () => h(RouterView) }), {
    global: { plugins: [router] },
  });
  await flushPromises();
  return { router, wrapper };
}

describe('leave guard', () => {
  it('blocks without asking when the page gives no confirmation', async () => {
    const { router, wrapper } = await setup(() => true);

    await router.push('/other');

    expect(router.currentRoute.value.path).toBe('/editor');
    wrapper.unmount();
  });

  it('asks first, and Stay keeps the page', async () => {
    const confirmLeave = vi.fn(async () => false);
    const { router, wrapper } = await setup(() => true, confirmLeave);

    await router.push('/other');

    expect(confirmLeave).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.path).toBe('/editor');
    expect(wrapper.text()).toBe('editor');
    wrapper.unmount();
  });

  it('asks first, and Leave continues to the intended route once',
    async () => {
      const confirmLeave = vi.fn(async () => true);
      const { router, wrapper } = await setup(() => true, confirmLeave);

      await router.push('/other');
      await flushPromises();

      expect(confirmLeave).toHaveBeenCalledTimes(1);
      expect(router.currentRoute.value.path).toBe('/other');
      expect(wrapper.text()).toBe('other');
      wrapper.unmount();
    });

  it('does not ask when nothing is unsaved', async () => {
    const confirmLeave = vi.fn(async () => false);
    const { router, wrapper } = await setup(() => false, confirmLeave);

    await router.push('/other');

    expect(confirmLeave).not.toHaveBeenCalled();
    expect(router.currentRoute.value.path).toBe('/other');
    wrapper.unmount();
  });
});
