import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { defineComponent, h, nextTick, ref } from 'vue';
import { renderToString } from 'vue/server-renderer';

import {
  SHOWCASE_TIMEOUT_MS,
  useShowcase,
} from '../../app/composables/useShowcase';
import type { ShowcaseQuery } from '../../app/lib/showcaseQuery';

// The listing loader: a request that outlasts its timeout fails, a newer
// request wins over an older answer, and leaving the page cancels the read
// (docs/design/showcase.md, Delivery, caching, and revocation).

const listing = (slug: string) => ({
  items: [{
    slug,
    cardVersion: '0123456789abcdef',
    imageText: slug,
    language: 'en',
    templateId: null,
    role: null,
  }],
  page: 1,
  pageCount: 1,
  total: 1,
});

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function harness() {
  const query = ref<ShowcaseQuery>({ page: 1 });
  const wrapper = mount(defineComponent({
    setup() {
      return { ...useShowcase(query) };
    },
    render: () => h('div'),
  }));
  const api = wrapper.vm as unknown as ReturnType<typeof useShowcase>;
  return { query, wrapper, api };
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('useShowcase', () => {
  it('fails a request that outlasts the timeout and aborts it', async () => {
    vi.useFakeTimers();
    let signal: AbortSignal | null | undefined;
    vi.stubGlobal('fetch', vi.fn((_url: string, init?: RequestInit) => {
      signal = init?.signal;
      return new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new Error('x')));
      });
    }));
    const { api } = harness();
    await nextTick();
    expect(api.view).toBe('loading');
    await vi.advanceTimersByTimeAsync(SHOWCASE_TIMEOUT_MS + 1);
    expect(signal?.aborted).toBe(true);
    expect(api.view).toBe('failed');
  });

  it('ignores an older answer that arrives after a newer request',
    async () => {
      const pending: ((response: Response) => void)[] = [];
      vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => {
        pending.push(resolve);
      })));
      const { query, api } = harness();
      await nextTick();
      query.value = { page: 1, role: 'qa' };
      await nextTick();
      expect(pending).toHaveLength(2);
      pending[1]!(json(listing('second')));
      await vi.waitFor(() => expect(api.view).toBe('list'));
      pending[0]!(json(listing('first')));
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(api.listing?.items[0]?.slug).toBe('second');
    });

  it('aborts the read when the page unmounts', async () => {
    let signal: AbortSignal | null | undefined;
    vi.stubGlobal('fetch', vi.fn((_url: string, init?: RequestInit) => {
      signal = init?.signal;
      return new Promise(() => {});
    }));
    const { wrapper } = harness();
    await nextTick();
    wrapper.unmount();
    expect(signal?.aborted).toBe(true);
  });

  it('renders only the loading state on the server, with no request',
    async () => {
      const fetcher = vi.fn();
      vi.stubGlobal('fetch', fetcher);
      const html = await renderToString(defineComponent({
        setup() {
          const { view } = useShowcase(ref<ShowcaseQuery>({ page: 1 }));
          return () => h('p', view.value);
        },
      }));
      expect(html).toContain('loading');
      expect(fetcher).not.toHaveBeenCalled();
    });
});
