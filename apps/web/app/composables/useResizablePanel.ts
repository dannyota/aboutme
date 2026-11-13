import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  watchEffect,
  type ComputedRef,
  type Ref,
} from 'vue';

export const EDITOR_PANEL_STORAGE_KEY = 'aboutme-editor-panel-width';
export const EDITOR_PANEL_CSS_VAR = '--panel-width';
export const EDITOR_PANEL_DEFAULT_REM = 22;
export const EDITOR_PANEL_MIN_REM = 22;
export const EDITOR_PANEL_MAX_REM = 48;
const EDITOR_PANEL_STEP_REM = 1;
const EDITOR_PANEL_LARGE_STEP_REM = 4;
const EDITOR_PANEL_SNAP_REM = 0.25;

const FIXED_COLUMNS_REM = 4 + 16.5;
export const EDITOR_PREVIEW_MIN_REM = 29.5;

export function maxPanelWidthRem(viewportRem: number): number {
  const roomForPanel = viewportRem - FIXED_COLUMNS_REM
    - EDITOR_PREVIEW_MIN_REM;
  return Math.min(
    EDITOR_PANEL_MAX_REM,
    Math.max(EDITOR_PANEL_MIN_REM, roomForPanel),
  );
}

export function clampPanelWidthRem(
  value: number,
  viewportRem: number,
): number {
  if (!Number.isFinite(value)) return EDITOR_PANEL_DEFAULT_REM;
  const max = maxPanelWidthRem(viewportRem);
  return Math.min(max, Math.max(EDITOR_PANEL_MIN_REM, value));
}

function snapToGridRem(value: number): number {
  return Math.round(value / EDITOR_PANEL_SNAP_REM) * EDITOR_PANEL_SNAP_REM;
}

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
  commit(): void;
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

export function createResizablePanelController(
  widthRem: Ref<number>,
  viewportRem: Readonly<Ref<number>>,
  onCommit?: (value: number) => void,
): ResizablePanelController {
  const maxRem = computed(() => maxPanelWidthRem(viewportRem.value));

  function setWidthRem(value: number): void {
    widthRem.value = clampPanelWidthRem(value, viewportRem.value);
  }
  function commitTo(value: number): void {
    setWidthRem(snapToGridRem(value));
    onCommit?.(widthRem.value);
  }
  function commit(): void {
    commitTo(widthRem.value);
  }
  function stepBy(deltaRem: number): void {
    commitTo(widthRem.value + deltaRem);
  }
  function reset(): void {
    commitTo(EDITOR_PANEL_DEFAULT_REM);
  }
  function goToMin(): void {
    commitTo(EDITOR_PANEL_MIN_REM);
  }
  function goToMax(): void {
    commitTo(maxRem.value);
  }

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
    commit,
    handleKeyDown,
    pxToRem: (px) => px / rootFontSizePx(),
  };
}

function readViewportRem(): number {
  if (typeof document === 'undefined') {
    return EDITOR_PANEL_MAX_REM + FIXED_COLUMNS_REM + EDITOR_PREVIEW_MIN_REM;
  }
  // clientWidth excludes the page scrollbar; innerWidth counts it (CSSOM).
  return document.documentElement.clientWidth / rootFontSizePx();
}

export function useResizablePanel(): ResizablePanelController {
  const widthRem = ref(EDITOR_PANEL_DEFAULT_REM);
  const viewportRem = ref(readViewportRem());
  let preferredWidthRem = EDITOR_PANEL_DEFAULT_REM;
  let hydrated = false;

  function readStoredRaw(): string | null {
    try {
      return window.localStorage.getItem(EDITOR_PANEL_STORAGE_KEY);
    } catch {
      return null;
    }
  }

  function persist(value: number): void {
    preferredWidthRem = value;
    if (!hydrated) return;
    try {
      window.localStorage.setItem(EDITOR_PANEL_STORAGE_KEY, String(value));
    } catch {
      // A blocked or full store keeps today's in-memory width for this tab.
    }
  }

  const controller = createResizablePanelController(
    widthRem,
    viewportRem,
    persist,
  );

  function handleResize(): void {
    viewportRem.value = readViewportRem();
    controller.setWidthRem(preferredWidthRem);
  }

  onMounted(() => {
    viewportRem.value = readViewportRem();
    const restored = parseStoredPanelWidthRem(
      readStoredRaw(),
      viewportRem.value,
    );
    preferredWidthRem = restored;
    widthRem.value = restored;
    hydrated = true;
    window.addEventListener('resize', handleResize);
  });

  onBeforeUnmount(() => {
    if (typeof window !== 'undefined') {
      window.removeEventListener('resize', handleResize);
    }
  });

  watchEffect(() => {
    if (typeof document === 'undefined') return;
    document.documentElement.style.setProperty(
      EDITOR_PANEL_CSS_VAR,
      `${widthRem.value}rem`,
    );
  });

  return controller;
}
