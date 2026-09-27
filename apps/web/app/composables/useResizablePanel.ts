import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
  watchEffect,
  type ComputedRef,
  type Ref,
} from 'vue';

/**
 * Resizable editor form panel (DESIGN.md "Authenticated chrome and editor":
 * the 22rem inspector in the four-region workspace). The panel defaults to
 * 22rem, the width it replaces, so a viewer with no stored choice sees
 * today's layout with no shift on first paint.
 */
export const EDITOR_PANEL_STORAGE_KEY = 'aboutme-editor-panel-width';
/** The custom property EditorShell.vue's grid reads for the 4th column. */
export const EDITOR_PANEL_CSS_VAR = '--panel-width';
export const EDITOR_PANEL_DEFAULT_REM = 22;
export const EDITOR_PANEL_MIN_REM = 22;
export const EDITOR_PANEL_MAX_REM = 48;
export const EDITOR_PANEL_STEP_REM = 1;
export const EDITOR_PANEL_LARGE_STEP_REM = 4;

/** The 4rem rail plus the 16.5rem outline (DESIGN.md's fixed columns). */
const FIXED_COLUMNS_REM = 4 + 16.5;
/** The preview's own floor in the four-region workspace (DESIGN.md). */
const PREVIEW_MIN_REM = 32;

/** The widest the panel can be while the preview keeps its 32rem floor. */
export function maxPanelWidthRem(viewportRem: number): number {
  const roomForPanel = viewportRem - FIXED_COLUMNS_REM - PREVIEW_MIN_REM;
  return Math.min(
    EDITOR_PANEL_MAX_REM,
    Math.max(EDITOR_PANEL_MIN_REM, roomForPanel),
  );
}

/** Pins a candidate width into today's [min, max] range for this viewport. */
export function clampPanelWidthRem(
  value: number,
  viewportRem: number,
): number {
  if (!Number.isFinite(value)) return EDITOR_PANEL_DEFAULT_REM;
  const max = maxPanelWidthRem(viewportRem);
  return Math.min(max, Math.max(EDITOR_PANEL_MIN_REM, value));
}

/**
 * Reads a stored width. A missing, unparsable, or out-of-range value falls
 * back to the default instead of clamping, so a width saved for a much wider
 * screen does not silently pin the panel to today's narrower maximum.
 */
export function parseStoredPanelWidthRem(
  raw: string | null,
  viewportRem: number,
): number {
  if (raw === null) return EDITOR_PANEL_DEFAULT_REM;
  const value = Number(raw);
  if (!Number.isFinite(value)) return EDITOR_PANEL_DEFAULT_REM;
  const max = maxPanelWidthRem(viewportRem);
  if (value < EDITOR_PANEL_MIN_REM || value > max) {
    return EDITOR_PANEL_DEFAULT_REM;
  }
  return value;
}

export interface ResizablePanelController {
  readonly widthRem: Ref<number>;
  readonly minRem: number;
  readonly maxRem: ComputedRef<number>;
  setWidthRem(value: number): void;
  stepBy(deltaRem: number): void;
  reset(): void;
  goToMin(): void;
  goToMax(): void;
  handleKeyDown(event: KeyboardEvent): void;
  pxToRem(px: number): number;
}

function rootFontSizePx(): number {
  if (typeof document === 'undefined') return 16;
  const parsed = Number.parseFloat(
    getComputedStyle(document.documentElement).fontSize,
  );
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 16;
}

/**
 * The DOM-free state and keyboard rules, kept separate from browser wiring
 * (`useResizablePanel` below) so they are testable without mounting.
 */
export function createResizablePanelController(
  widthRem: Ref<number>,
  viewportRem: Readonly<Ref<number>>,
): ResizablePanelController {
  const maxRem = computed(() => maxPanelWidthRem(viewportRem.value));

  function setWidthRem(value: number): void {
    widthRem.value = clampPanelWidthRem(value, viewportRem.value);
  }
  function stepBy(deltaRem: number): void {
    setWidthRem(widthRem.value + deltaRem);
  }
  function reset(): void {
    setWidthRem(EDITOR_PANEL_DEFAULT_REM);
  }
  function goToMin(): void {
    setWidthRem(EDITOR_PANEL_MIN_REM);
  }
  function goToMax(): void {
    setWidthRem(maxRem.value);
  }

  // The panel sits to the right of the preview, so moving the pointer or
  // pressing Left grows it (taking room from the preview) and Right shrinks
  // it back, matching a drag on the panel's left edge.
  function handleKeyDown(event: KeyboardEvent): void {
    const step = event.shiftKey
      ? EDITOR_PANEL_LARGE_STEP_REM
      : EDITOR_PANEL_STEP_REM;
    switch (event.key) {
      case 'ArrowLeft':
        event.preventDefault();
        stepBy(step);
        return;
      case 'ArrowRight':
        event.preventDefault();
        stepBy(-step);
        return;
      case 'Home':
        event.preventDefault();
        goToMin();
        return;
      case 'End':
        event.preventDefault();
        goToMax();
        return;
      case 'Enter':
        event.preventDefault();
        reset();
        return;
      default:
    }
  }

  return {
    widthRem,
    minRem: EDITOR_PANEL_MIN_REM,
    maxRem,
    setWidthRem,
    stepBy,
    reset,
    goToMin,
    goToMax,
    handleKeyDown,
    pxToRem: (px) => px / rootFontSizePx(),
  };
}

function readViewportRem(): number {
  if (typeof window === 'undefined') {
    // No layout depends on this before hydration; a generous sentinel keeps
    // the default bounds from clamping unnecessarily.
    return EDITOR_PANEL_MAX_REM + FIXED_COLUMNS_REM + PREVIEW_MIN_REM;
  }
  return window.innerWidth / rootFontSizePx();
}

/**
 * Wires the pure controller to the browser: restores and persists the width
 * in `localStorage` (no server storage for this per-viewer preference), and
 * re-clamps on window resize so the preview keeps its floor.
 */
export function useResizablePanel(): ResizablePanelController {
  const widthRem = ref(EDITOR_PANEL_DEFAULT_REM);
  const viewportRem = ref(readViewportRem());
  const controller = createResizablePanelController(widthRem, viewportRem);
  let hydrated = false;

  function handleResize(): void {
    viewportRem.value = readViewportRem();
    controller.setWidthRem(widthRem.value);
  }

  function readStoredRaw(): string | null {
    try {
      return window.localStorage.getItem(EDITOR_PANEL_STORAGE_KEY);
    } catch {
      return null;
    }
  }

  onMounted(() => {
    viewportRem.value = readViewportRem();
    widthRem.value = parseStoredPanelWidthRem(
      readStoredRaw(),
      viewportRem.value,
    );
    hydrated = true;
    window.addEventListener('resize', handleResize);
  });

  onBeforeUnmount(() => {
    if (typeof window !== 'undefined') {
      window.removeEventListener('resize', handleResize);
    }
  });

  // Written on the document root (like useTheme's data-theme) so the grid
  // in EditorShell.vue can read it without this composable's caller
  // threading the width through props.
  watchEffect(() => {
    if (typeof document === 'undefined') return;
    document.documentElement.style.setProperty(
      EDITOR_PANEL_CSS_VAR,
      `${widthRem.value}rem`,
    );
  });

  watch(widthRem, (value) => {
    // Skip the write before the stored value has been read, so an unread
    // default never overwrites a real preference.
    if (!hydrated) return;
    try {
      window.localStorage.setItem(EDITOR_PANEL_STORAGE_KEY, String(value));
    } catch {
      // A blocked or full store keeps today's in-memory width for this tab.
    }
  });

  return controller;
}
