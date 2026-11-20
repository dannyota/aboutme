<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import {
  type ComponentPublicInstance,
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from 'vue';

import PagedResume from '../resume/PagedResume.vue';
import { resolvePageGeometry } from '../resume/pageMetrics';
import type { RenderContext } from '../resume/resolveRenderModel';
import { renderPageRule } from '../resume/useResumeStyles';
import { fontsReady } from '../../utils/fontsReady';
import { removePageRule, syncPageRule } from '../../public/pageRule';
import { bindPageScroller, type PageScroller } from '../../public/pageScroller';
import {
  clampPageIndex,
  createPagesController,
  PAGE_CHANGE_EVENT,
  pageScale,
  type PagesState,
  PHONE_QUERY,
} from '../../public/publicPages';
import PublicPager from './PublicPager.vue';
import { publicPagesCopy } from './publicPagesCopy';

const props = defineProps<{
  document: Resume;
  context: RenderContext;
}>();
const emit = defineEmits<{ state: [state: PagesState] }>();

const copy = computed(() => publicPagesCopy(props.context.lng));
const geometry = computed(() =>
  resolvePageGeometry(props.document.customization));
const state = ref<PagesState>('loading');
const count = ref(0);
const index = ref(0);
const announced = ref(0);
const phone = ref(false);
const scale = ref(1);
const root = ref<HTMLElement>();
const paged = shallowRef<ComponentPublicInstance | null>(null);
const hidden = computed(() =>
  state.value === 'loading' || state.value === 'staging');
const live = computed(() => count.value > 0
  ? copy.value.page(announced.value + 1, count.value)
  : '');
let scroller: PageScroller | undefined;
let phoneQuery: MediaQueryList | undefined;
let focusInside = false;

const scrollerElement = (): HTMLElement | undefined => {
  const element = paged.value?.$el as unknown;
  return element instanceof HTMLElement ? element : undefined;
};

const withinFirstPage = (): boolean => {
  const sheet = root.value?.querySelector<HTMLElement>('.public-page-sheet');
  const anchor = root.value?.parentElement?.querySelector<HTMLElement>(
    ':scope > .resume-document',
  ) ?? root.value;
  if (sheet === undefined || sheet === null || anchor === undefined) {
    return true;
  }
  const top = anchor.getBoundingClientRect().top + window.scrollY;
  return window.scrollY <= top + sheet.offsetHeight;
};

const reducedMotion = (): boolean =>
  window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;

const bindScroller = (): void => {
  const element = scrollerElement();
  if (element === undefined) {
    scroller?.dispose();
    scroller = undefined;
    return;
  }
  if (scroller === undefined) {
    scroller = bindPageScroller(element, {
      onIndex: (value) => {
        index.value = clampPageIndex(value, count.value);
      },
      onSettle: (value) => {
        index.value = clampPageIndex(value, count.value);
        announced.value = index.value;
      },
    });
  }
  if (phone.value) scroller.observe();
};

const afterPages = (update: boolean): void => {
  bindScroller();
  if (phone.value) scroller?.scrollTo(index.value, false);
  if (update && focusInside && phone.value) {
    scrollerElement()?.focus({ preventScroll: true });
  }
  focusInside = false;
};

const controller = createPagesController({
  withinFirstPage,
  onState: (next) => {
    state.value = next;
    emit('state', next);
    if (next === 'continuous') {
      scroller?.dispose();
      scroller = undefined;
    }
  },
  onPages: (pages, update) => {
    count.value = pages;
    index.value = clampPageIndex(index.value, pages);
    announced.value = index.value;
    void nextTick(() => afterPages(update));
  },
});

const contentWidth = (): number => {
  const page = root.value?.parentElement?.clientWidth ?? 0;
  return page > 0 ? page : window.document.documentElement.clientWidth;
};

const measureViewport = (): void => {
  const wasPhone = phone.value;
  phone.value = phoneQuery?.matches === true;
  scale.value = pageScale(contentWidth(), geometry.value.widthPx);
  if (phone.value && !wasPhone) scroller?.observe();
};

const move = (delta: number): void => {
  if (count.value < 2) return;
  const target = clampPageIndex(index.value + delta, count.value);
  if (target === index.value) return;
  index.value = target;
  scroller?.scrollTo(target, !reducedMotion());
};

const onKeydown = (event: KeyboardEvent): void => {
  if (!phone.value) return;
  if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
  event.preventDefault();
  move(event.key === 'ArrowRight' ? 1 : -1);
};

const loadFonts = (): Promise<void> =>
  fontsReady(props.document.customization.font.family);

watch(index, () => {
  root.value?.dispatchEvent(new CustomEvent(PAGE_CHANGE_EVENT, {
    bubbles: true,
  }));
}, { flush: 'post' });

watch(() => renderPageRule(geometry.value), (rule) => {
  syncPageRule(window.document, rule);
});

watch(() => [props.document, props.context] as const, () => {
  const active = window.document.activeElement;
  focusInside = active === null || active === window.document.body
    || root.value?.contains(active) === true;
  measureViewport();
  controller.load(loadFonts());
});

onMounted(() => {
  phoneQuery = window.matchMedia?.(PHONE_QUERY);
  phoneQuery?.addEventListener?.('change', measureViewport);
  window.addEventListener('resize', measureViewport, { passive: true });
  measureViewport();
  syncPageRule(window.document, renderPageRule(geometry.value));
  emit('state', state.value);
  controller.load(loadFonts());
});

onBeforeUnmount(() => {
  controller.dispose();
  scroller?.dispose();
  phoneQuery?.removeEventListener?.('change', measureViewport);
  window.removeEventListener('resize', measureViewport);
  removePageRule(window.document);
});
</script>

<template>
  <div
    v-if="state !== 'continuous'"
    ref="root"
    class="public-pages"
    :data-state="state"
    :data-page-index="index"
    :data-page-count="count"
    :inert="hidden ? true : undefined"
    :aria-hidden="hidden ? 'true' : undefined"
  >
    <PagedResume
      v-if="state !== 'loading'"
      ref="paged"
      :document="document"
      :context="context"
      role="region"
      :aria-label="copy.pages"
      :tabindex="phone ? 0 : undefined"
      @settled="controller.settled($event)"
      @failed="controller.failed()"
      @keydown="onKeydown"
    >
      <template
        #page="{ index: page, count: pages, widthPx, heightPx, page: sheet }"
      >
        <section
          class="public-page"
          :aria-label="copy.page(page + 1, pages)"
          :data-page-index="page"
        >
          <div
            class="public-page-sheet"
            :style="{
              width: `${widthPx * scale}px`,
              height: `${heightPx * scale}px`,
            }"
          >
            <div
              class="public-page-scale"
              :style="{
                'width': `${widthPx}px`,
                'height': `${heightPx}px`,
                'transform': `scale(${scale})`,
                '--public-scale': String(scale),
              }"
            >
              <component :is="sheet" />
            </div>
          </div>
          <p
            class="public-page-position"
            aria-hidden="true"
          >
            {{ page + 1 }} / {{ pages }}
          </p>
        </section>
      </template>
    </PagedResume>
    <PublicPager
      v-if="count > 1 && !hidden"
      :index="index"
      :count="count"
      :copy="copy"
      @move="move"
    />
    <p
      class="public-pages-live"
      aria-live="polite"
    >
      {{ live }}
    </p>
  </div>
</template>
