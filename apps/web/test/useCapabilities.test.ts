import { beforeEach, describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import { useCapabilities } from '../app/composables/useCapabilities';
import { registerCapabilities } from './support/capabilities';

const Probe = defineComponent({
  setup() {
    const { providerLogin, agentAccess, resolved } = useCapabilities();
    return () =>
      h('div', {
        'data-provider': String(providerLogin.value),
        'data-agent': String(agentAccess.value),
        'data-resolved': String(resolved.value),
      });
  },
});

async function probe(): Promise<Record<string, string | undefined>> {
  const wrapper = await mountSuspended(Probe);
  await flushPromises();
  const el = wrapper.get('div');
  return {
    provider: el.attributes('data-provider'),
    agent: el.attributes('data-agent'),
    resolved: el.attributes('data-resolved'),
  };
}

describe('useCapabilities', () => {
  beforeEach(() => {
    clearNuxtData();
  });

  it('reflects both flags from the server', async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    expect(await probe()).toEqual({
      provider: 'true',
      agent: 'false',
      resolved: 'true',
    });
  });

  it('treats a failed read as all false', async () => {
    registerCapabilities(null);
    expect(await probe()).toEqual({
      provider: 'false',
      agent: 'false',
      resolved: 'true',
    });
  });

  it('treats a malformed body as all false', async () => {
    registerCapabilities({
      providerLogin: 'yes' as unknown as boolean,
      agentAccess: 1 as unknown as boolean,
    });
    expect(await probe()).toEqual({
      provider: 'false',
      agent: 'false',
      resolved: 'true',
    });
  });

  it.each([
    ['an empty body', {}],
    ['a null data envelope', { data: null }],
  ])('treats %s as all false', async (_label, body) => {
    registerEndpoint('/api/v1/capabilities', () => body);
    expect(await probe()).toEqual({
      provider: 'false',
      agent: 'false',
      resolved: 'true',
    });
  });
});
