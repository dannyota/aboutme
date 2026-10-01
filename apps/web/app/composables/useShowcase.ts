import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  type Ref,
  shallowRef,
  watch,
} from 'vue';

import {
  parseShowcaseListing,
  SHOWCASE_LISTING_PATH,
  type ShowcaseListing,
} from '../lib/showcaseContract';
import {
  hasFilters,
  type ShowcaseQuery,
  showcaseSearch,
} from '../lib/showcaseQuery';

export type ShowcaseView = 'loading' | 'list' | 'empty' | 'no-match' | 'failed';

/** A listing request that outlasts this fails like any other error. */
export const SHOWCASE_TIMEOUT_MS = 10_000;

/**
 * Loads the community listing in the browser, after hydration: the server
 * renders only the loading state. Nothing is cached or stored, and the
 * request omits credentials, so no cookie travels with it
 * (docs/design/showcase.md, Delivery, caching, and revocation).
 */
export function useShowcase(query: Readonly<Ref<ShowcaseQuery>>) {
  const listing = shallowRef<ShowcaseListing | null>(null);
  const status = ref<'loading' | 'ready' | 'failed'>('loading');
  const retrying = ref(false);
  // Counts finished loads, so the page can move focus once one settles.
  const settled = ref(0);
  let started = false;
  let sequence = 0;
  let controller: AbortController | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function stop(): void {
    controller?.abort();
    controller = null;
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  }

  async function load(keepBanner: boolean): Promise<void> {
    stop();
    const mine = ++sequence;
    const current = new AbortController();
    controller = current;
    timer = setTimeout(() => current.abort(), SHOWCASE_TIMEOUT_MS);
    if (keepBanner) {
      retrying.value = true;
    } else {
      status.value = 'loading';
    }
    const search = showcaseSearch(query.value);
    const url = search === ''
      ? SHOWCASE_LISTING_PATH
      : `${SHOWCASE_LISTING_PATH}?${search}`;
    try {
      const response = await fetch(
        url,
        {
          cache: 'no-store',
          credentials: 'omit',
          headers: { Accept: 'application/json' },
          signal: current.signal,
        },
      );
      if (!response.ok) throw new Error(`showcase ${response.status}`);
      const parsed = parseShowcaseListing(await response.json());
      if (mine !== sequence) return;
      listing.value = parsed;
      status.value = 'ready';
    } catch {
      if (mine !== sequence) return;
      listing.value = null;
      status.value = 'failed';
    } finally {
      if (mine === sequence) {
        if (timer !== undefined) clearTimeout(timer);
        timer = undefined;
        retrying.value = false;
        settled.value += 1;
      }
    }
  }

  const view = computed<ShowcaseView>(() => {
    if (status.value !== 'ready' || listing.value === null) {
      return status.value === 'failed' ? 'failed' : 'loading';
    }
    if (listing.value.items.length > 0) return 'list';
    return listing.value.total === 0 && !hasFilters(query.value)
      ? 'empty'
      : 'no-match';
  });

  onMounted(() => {
    started = true;
    void load(false);
  });
  watch(
    () => showcaseSearch(query.value),
    () => {
      if (started) void load(false);
    },
  );
  onBeforeUnmount(() => {
    sequence += 1;
    stop();
  });

  return {
    view,
    listing,
    retrying,
    settled,
    retry: (): void => {
      if (!retrying.value) void load(true);
    },
  };
}
