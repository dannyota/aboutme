<script lang="ts">
import type { Resume, Section } from '@aboutme/schema';
import {
  type CSSProperties,
  defineComponent,
  h,
  inject,
  nextTick,
  onBeforeUnmount,
  onMounted,
  type PropType,
  ref,
  shallowRef,
  type ShallowRef,
  toRaw,
  watch,
} from 'vue';

import SectionRenderer from './SectionRenderer.vue';
import ResumeHeader from './ResumeHeader.vue';
import {
  measurePagination,
  type MeasuredLayout,
  PaginationMeasureKey,
} from './measure';
import { pageContentHeightPx } from './pageMetrics';
import {
  type BlockRef,
  type Page,
  PaginationError,
  type PaginationRequest,
  paginate,
} from './paginate';
import {
  type RenderContext,
  type ResolvedRenderModel,
  resolveRenderModel,
} from './resolveRenderModel';

interface PaginationSnapshot {
  model: ResolvedRenderModel;
  request: PaginationRequest;
}

interface RenderedPaginationSnapshot extends PaginationSnapshot {
  pages: readonly Page[];
}

const visibleEntries = (section: Section): number[] =>
  section.entries.flatMap((entry, index) => entry.isHidden ? [] : [index]);

const buildRequest = (
  document: Resume,
  context: RenderContext,
  model: ResolvedRenderModel,
): PaginationRequest => {
  const flows = model.columns === 1
    ? [{ column: 'main' as const, sections: [...model.main, ...model.sidebar] }]
    : [
        { column: 'main' as const, sections: model.main },
        { column: 'sidebar' as const, sections: model.sidebar },
      ];
  const blocks = flows.flatMap(({ column, sections }): BlockRef[] =>
    sections.flatMap(({ key, section }) => {
      const indices = visibleEntries(section);
      if (indices.length === 0) return [];
      return [
        { sectionKey: key, kind: 'heading', column },
        ...indices.map((entryIndex): BlockRef => ({
          sectionKey: key,
          kind: 'entry',
          entryIndex,
          column,
        })),
      ];
    }),
  );
  return {
    document,
    context,
    columns: model.columns,
    blocks,
    page: model.styles.page,
  };
};

const assertLayout = (
  request: PaginationRequest,
  layout: MeasuredLayout,
): void => {
  if (
    layout.columns !== request.columns
    || layout.blocks.length !== request.blocks.length
  ) {
    throw new PaginationError(
      'invalid_measurement',
      'Measured pagination layout does not match the request.',
    );
  }
  for (const [index, expected] of request.blocks.entries()) {
    const actual = layout.blocks[index];
    if (
      actual?.sectionKey !== expected.sectionKey
      || actual.kind !== expected.kind
      || actual.entryIndex !== expected.entryIndex
      || actual.column !== expected.column
    ) {
      throw new PaginationError(
        'invalid_measurement',
        `Measured pagination block ${index} does not match the request.`,
      );
    }
  }
};

const pagesFrom = (
  request: PaginationRequest,
  layout: MeasuredLayout,
): Page[] => {
  assertLayout(request, layout);
  return paginate(
    layout.blocks,
    layout.columns,
    pageContentHeightPx(request.page),
    layout.headerHeightPx,
    layout.headerBodyGapPx,
  );
};

const sectionIndex = (
  model: ResolvedRenderModel,
): ReadonlyMap<string, Section> => new Map(
  [...model.main, ...model.sidebar].map(({ key, section }) => [key, section]),
);

const sectionSlice = (
  sections: ReadonlyMap<string, Section>,
  block: BlockRef,
): Section => {
  const section = sections.get(block.sectionKey);
  if (section === undefined) {
    throw new PaginationError(
      'invalid_measurement',
      `Pagination references missing section ${block.sectionKey}.`,
    );
  }
  if (block.kind === 'heading') return section;
  if (
    block.entryIndex === undefined
    || section.entries[block.entryIndex] === undefined
  ) {
    throw new PaginationError(
      'invalid_measurement',
      `Pagination references missing entry ${block.sectionKey}.`,
    );
  }
  const clone = structuredClone(toRaw(section));
  clone.entries.splice(block.entryIndex + 1);
  clone.entries.splice(0, block.entryIndex);
  return clone;
};

// Paged preview slices measured entry blocks; the PDF remains authoritative.
export default defineComponent({
  name: 'PagedResume',
  props: {
    document: { type: Object as PropType<Resume>, required: true },
    context: { type: Object as PropType<RenderContext>, required: true },
  },
  setup(props) {
    const measure = inject(PaginationMeasureKey, null);
    const snapshot = (): PaginationSnapshot => {
      const model = resolveRenderModel(props.document, props.context);
      return {
        model,
        request: buildRequest(props.document, props.context, model),
      };
    };

    const renderReady = (
      rendered: RenderedPaginationSnapshot,
      settled: boolean,
      measurementRoot?: ShallowRef<HTMLElement | null>,
      measured: PaginationSnapshot = rendered,
    ) => {
      const { model, request, pages } = rendered;
      const sections = sectionIndex(model);
      const page = request.page;
      const rootStyle: CSSProperties = {
        ...model.styles.root,
        fontSynthesis: 'none',
        printColorAdjust: 'exact',
        WebkitPrintColorAdjust: 'exact',
      };
      const renderSection = (
        targetModel: ResolvedRenderModel,
        targetSections: ReadonlyMap<string, Section>,
        block: BlockRef,
        appliedGapBeforePx: number,
        index?: number,
      ) => h('div', {
        'class': 'pagination-atomic',
        'data-pagination-block-index': index,
        'data-section-key': block.sectionKey,
        'data-block-kind': block.kind,
        'data-block-overflow':
          'overflow' in block && block.overflow ? 'true' : undefined,
        'style': { marginBlockStart: `${appliedGapBeforePx}px` },
      }, [h(SectionRenderer, {
        section: sectionSlice(targetSections, block),
        dateFormat: targetModel.dateFormat,
        sectionDisplay: targetModel.sectionDisplay,
        renderPart: block.kind,
      })]);
      const renderHeader = (
        targetModel: ResolvedRenderModel,
        measurement = false,
      ) => h('div', {
        'class': 'pagination-header',
        'data-pagination-header': measurement ? '' : undefined,
        'style': targetModel.styles.header,
      }, [h(ResumeHeader, {
        personalDetails: targetModel.personalDetails,
        header: targetModel.header,
        photo: targetModel.photo,
      })]);
      const renderColumns = (
        targetModel: ResolvedRenderModel,
        targetRequest: PaginationRequest,
        targetSections: ReadonlyMap<string, Section>,
        main: readonly BlockRef[],
        sidebar: readonly BlockRef[],
        gaps: ReadonlyMap<BlockRef, number>,
        indexed = false,
      ) => targetModel.columns === 1
        ? h('div', { class: 'layout-one-column' }, main.map((block) =>
            renderSection(
              targetModel,
              targetSections,
              block,
              gaps.get(block) ?? 0,
              indexed ? targetRequest.blocks.indexOf(block) : undefined,
            )))
        : h('div', { class: 'layout-two-columns' }, [
            h('main', { class: 'resume-main' }, main.map((block) =>
              renderSection(
                targetModel,
                targetSections,
                block,
                gaps.get(block) ?? 0,
                indexed ? targetRequest.blocks.indexOf(block) : undefined,
              ))),
            h('aside', {
              class: 'resume-sidebar',
              style: targetModel.styles.sidebar,
            }, sidebar.map((block) => renderSection(
              targetModel,
              targetSections,
              block,
              gaps.get(block) ?? 0,
              indexed ? targetRequest.blocks.indexOf(block) : undefined,
            ))),
          ]);
      const visiblePages = pages.map((placedPage, pageIndex) => {
        const expandedHeightPx = Math.max(
          page.heightPx,
          ((2 * page.marginYmm * 96) / 25.4) + placedPage.contentHeightPx,
        );
        const gaps = new Map<BlockRef, number>();
        for (const block of [...placedPage.main, ...placedPage.sidebar]) {
          gaps.set(block, block.appliedGapBeforePx);
        }
        return h('article', {
          'class': ['resume-document', 'resume-page'],
          'key': pageIndex,
          'lang': model.lng,
          'style': {
            ...rootStyle,
            width: `${page.widthPx}px`,
            minHeight: `${page.heightPx}px`,
            height: `${expandedHeightPx}px`,
          },
          'data-page-index': pageIndex,
          'data-page-overflow': placedPage.overflow ? 'true' : 'false',
        }, [
          ...(placedPage.header === undefined
            ? []
            : [h('div', {
                style: {
                  marginBlockEnd: `${placedPage.header.bodyGapPx}px`,
                },
              }, [renderHeader(model)])]),
          renderColumns(
            model,
            request,
            sections,
            placedPage.main,
            placedPage.sidebar,
            gaps,
          ),
        ]);
      });
      const measuredModel = measured.model;
      const measuredRequest = measured.request;
      const measuredSections = sectionIndex(measuredModel);
      const measuredPage = measuredRequest.page;
      const measuredRootStyle: CSSProperties = {
        ...measuredModel.styles.root,
        fontSynthesis: 'none',
        printColorAdjust: 'exact',
        WebkitPrintColorAdjust: 'exact',
      };
      const hiddenGaps = new Map<BlockRef, number>();
      const hiddenMain = measuredRequest.blocks.filter(
        (block) => block.column === 'main',
      );
      const hiddenSidebar = measuredRequest.blocks.filter(
        (block) => block.column === 'sidebar',
      );
      const measurement = measurementRoot === undefined
        ? []
        : [h('article', {
            'ref': measurementRoot,
            'class': [
              'resume-document',
              'resume-page',
              'pagination-measurement',
            ],
            'aria-hidden': 'true',
            'inert': '',
            'style': {
              ...measuredRootStyle,
              width: `${measuredPage.widthPx}px`,
              minHeight: `${measuredPage.heightPx}px`,
            },
          }, [
            renderHeader(measuredModel, true),
            renderColumns(
              measuredModel,
              measuredRequest,
              measuredSections,
              hiddenMain,
              hiddenSidebar,
              hiddenGaps,
              true,
            ),
          ])];
      return h('div', {
        'class': 'paged-resume',
        'data-render-mode': 'paged',
        'data-pagination-settled': settled ? 'true' : undefined,
      }, [...visiblePages, ...measurement]);
    };

    if (measure !== null) {
      const { model, request } = snapshot();
      return Promise.resolve(measure(request)).then((layout) => {
        const pages = pagesFrom(request, layout);
        return () => renderReady({ model, request, pages }, true);
      });
    }

    if (import.meta.server || typeof document === 'undefined') {
      throw new PaginationError(
        'pagination_measurement_required',
        'Paged SSR requires an injected pagination measurer.',
      );
    }

    const measurementRoot = shallowRef<HTMLElement | null>(null);
    const initial = snapshot();
    const rendered = ref<RenderedPaginationSnapshot>({
      ...initial,
      pages: [],
    });
    const measurement = ref<PaginationSnapshot>(initial);
    let desired = initial;
    const settled = ref(false);
    let generation = 0;
    let measuring = false;
    let queued = false;
    let frame = 0;
    let active = true;
    let observer: ResizeObserver | undefined;
    let observedSizes = new WeakMap<Element, string>();

    const bindObserver = (): void => {
      const root = measurementRoot.value;
      if (!active || root === null || observer === undefined) return;
      observer.disconnect();
      observedSizes = new WeakMap<Element, string>();
      for (const element of root.querySelectorAll(
        '[data-pagination-header], [data-pagination-block-index]',
      )) {
        const rect = element.getBoundingClientRect();
        observedSizes.set(element, `${rect.width}:${rect.height}`);
        observer.observe(element);
      }
    };

    const run = async (): Promise<void> => {
      if (measuring) {
        queued = true;
        return;
      }
      const root = measurementRoot.value;
      if (!active || root === null) return;
      measuring = true;
      settled.value = false;
      const currentGeneration = ++generation;
      measurement.value = desired;
      const current = measurement.value;
      await nextTick();
      try {
        const layout = await measurePagination(root, current.request);
        if (!active || currentGeneration !== generation) return;
        rendered.value = {
          ...current,
          pages: pagesFrom(current.request, layout),
        };
        await nextTick();
        if (active && currentGeneration === generation) {
          bindObserver();
          settled.value = true;
        }
      } finally {
        measuring = false;
        if (queued) {
          queued = false;
          schedule();
        }
      }
    };
    const schedule = (): void => {
      if (!active) return;
      settled.value = false;
      generation += 1;
      if (measuring) {
        queued = true;
        return;
      }
      if (frame !== 0) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        void run();
      });
    };
    const onFontsChanged = (): void => schedule();

    onMounted(async () => {
      await run();
      const root = measurementRoot.value;
      if (root === null) return;
      observer = new ResizeObserver((entries) => {
        const changed = entries.some((entry) => {
          const size = `${entry.contentRect.width}:${entry.contentRect.height}`;
          if (observedSizes.get(entry.target) === size) return false;
          observedSizes.set(entry.target, size);
          return true;
        });
        if (changed) schedule();
      });
      bindObserver();
      document.fonts.addEventListener('loadingdone', onFontsChanged);
    });
    watch(
      () => [props.document, props.context] as const,
      () => {
        desired = snapshot();
        schedule();
      },
      { deep: true, flush: 'post' },
    );
    onBeforeUnmount(() => {
      active = false;
      queued = false;
      generation += 1;
      observer?.disconnect();
      document.fonts.removeEventListener('loadingdone', onFontsChanged);
      if (frame !== 0) cancelAnimationFrame(frame);
    });

    return () => renderReady(
      rendered.value,
      settled.value,
      measurementRoot,
      measurement.value,
    );
  },
});
</script>

<style>
.paged-resume {
  display: grid;
  gap: 24px;
  justify-content: center;
}

.resume-page {
  overflow: visible;
}

.resume-page[data-page-overflow='true'] {
  outline: 2px solid #b42318;
  outline-offset: 2px;
}

.pagination-atomic[data-block-overflow='true'] {
  outline: 2px dashed #b42318;
  outline-offset: 2px;
}

.pagination-measurement {
  position: fixed;
  inset: 0 auto auto -100000px;
  z-index: -1;
  visibility: hidden;
  pointer-events: none;
}

.resume-document.resume-page .resume-header,
.resume-document .pagination-atomic .resume-section,
.resume-document .pagination-atomic .section-heading,
.resume-document .pagination-atomic .entry {
  margin-block-end: 0;
}

.resume-document .pagination-atomic .section-heading:empty {
  min-block-size: 1px;
}
</style>
