import { describe, expect, it, vi } from 'vitest';

import {
  observeSettledVisiblePageCount,
  type PageCountObserverDeps,
} from '../../app/editor/pageCountObserver';

describe('observeSettledVisiblePageCount', () => {
  it('reports only a settled contiguous visible page set', () => {
    const root = document.createElement('div');
    const onSettled = vi.fn();
    let mutation: MutationCallback = () => undefined;
    const callbacks = new Map<number, FrameRequestCallback>();
    let nextFrame = 0;
    const disconnect = vi.fn();
    const deps: PageCountObserverDeps = {
      requestAnimationFrame: (callback) => {
        callbacks.set(++nextFrame, callback);
        return nextFrame;
      },
      cancelAnimationFrame: (handle) => void callbacks.delete(handle),
      createMutationObserver: (callback) => {
        mutation = callback;
        return { observe: vi.fn(), disconnect };
      },
      isVisible: (page) => !page.hidden
        && page.getAttribute('aria-hidden') !== 'true'
        && !page.hasAttribute('inert'),
    };
    const runNext = () => {
      const next = callbacks.entries().next().value as
        | [number, FrameRequestCallback]
        | undefined;
      expect(next).toBeDefined();
      const [handle, callback] = next!;
      callbacks.delete(handle);
      callback(0);
    };
    root.innerHTML = [
      '<article class="resume-page" data-page-index="0"></article>',
      '<article class="resume-page" data-page-index="1"></article>',
      '<article class="resume-page pagination-measurement" '
      + 'data-page-index="2"></article>',
      '<article class="resume-page" data-page-index="8" hidden></article>',
    ].join('');

    const stop = observeSettledVisiblePageCount(root, onSettled, deps);
    runNext();
    mutation([], {} as MutationObserver);
    expect(onSettled).not.toHaveBeenCalled();
    while (callbacks.size > 0) runNext();
    expect(onSettled).toHaveBeenCalledExactlyOnceWith(2);

    stop();
    expect(disconnect).toHaveBeenCalledOnce();
    expect(callbacks.size).toBe(0);
  });

  it('does not report a noncontiguous page set', () => {
    const root = document.createElement('div');
    root.innerHTML = [
      '<article class="resume-page" data-page-index="0"></article>',
      '<article class="resume-page" data-page-index="2"></article>',
    ].join('');
    const callbacks: FrameRequestCallback[] = [];
    const onSettled = vi.fn();

    const stop = observeSettledVisiblePageCount(root, onSettled, {
      requestAnimationFrame: (callback) => callbacks.push(callback),
      cancelAnimationFrame: vi.fn(),
      createMutationObserver: () => ({
        observe: vi.fn(),
        disconnect: vi.fn(),
      }),
      isVisible: () => true,
    });
    callbacks.shift()?.(0);

    expect(onSettled).not.toHaveBeenCalled();
    stop();
  });
});
