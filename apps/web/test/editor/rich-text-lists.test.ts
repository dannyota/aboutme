// @vitest-environment jsdom

import { mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import RichTextEditor from
  '../../app/components/editor/richtext/RichTextEditor.vue';

type Wrapper = ReturnType<typeof mount>;

const rect = {
  bottom: 10,
  height: 10,
  left: 0,
  right: 10,
  top: 0,
  width: 10,
  x: 0,
  y: 0,
} as DOMRect;
const restores: (() => void)[] = [];

function stubRects(prototype: object): void {
  const descriptor = Object.getOwnPropertyDescriptor(
    prototype,
    'getClientRects',
  );
  Object.defineProperty(prototype, 'getClientRects', {
    configurable: true,
    value: () => [rect],
  });
  restores.push(() => {
    if (descriptor === undefined) {
      delete (prototype as { getClientRects?: unknown }).getClientRects;
    } else {
      Object.defineProperty(prototype, 'getClientRects', descriptor);
    }
  });
}

beforeEach(() => {
  // ProseMirror measures the caret to scroll it into view after a command.
  stubRects(Text.prototype);
  stubRects(Range.prototype);
  stubRects(HTMLElement.prototype);
  const scrollBy = vi.spyOn(window, 'scrollBy').mockImplementation(() => {});
  restores.push(() => scrollBy.mockRestore());
});

afterEach(() => {
  while (restores.length > 0) restores.pop()?.();
  document.body.innerHTML = '';
});

function mountEditor(modelValue: string): Wrapper {
  return mount(RichTextEditor, {
    attachTo: document.body,
    props: { modelValue },
  });
}

function editorOf(wrapper: Wrapper): HTMLElement {
  return wrapper.get('[contenteditable="true"]').element as HTMLElement;
}

/** Puts the caret at the end of the last text node that reads `text`. */
async function caretAfter(editor: HTMLElement, text: string): Promise<void> {
  const walker = document.createTreeWalker(editor, NodeFilter.SHOW_TEXT);
  let target: Text | undefined;
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (node.textContent === text) target = node as Text;
  }
  if (target === undefined) throw new Error(`No text node reads "${text}".`);
  editor.focus();
  const range = document.createRange();
  range.setStart(target, text.length);
  range.collapse(true);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
  document.dispatchEvent(new Event('selectionchange'));
  await new Promise((resolve) => setTimeout(resolve, 20));
}

async function press(
  editor: HTMLElement,
  key: string,
  modifiers: { shiftKey?: boolean } = {},
): Promise<KeyboardEvent> {
  const event = new KeyboardEvent('keydown', {
    bubbles: true,
    cancelable: true,
    key,
    ...modifiers,
  });
  editor.dispatchEvent(event);
  await new Promise((resolve) => setTimeout(resolve, 0));
  return event;
}

async function typeText(editor: HTMLElement, text: string): Promise<void> {
  const paste = new Event('paste', { cancelable: true });
  Object.defineProperty(paste, 'clipboardData', {
    value: {
      files: [],
      getData: (kind: string) => (kind === 'text/plain' ? text : ''),
    },
  });
  editor.dispatchEvent(paste);
  await new Promise((resolve) => setTimeout(resolve, 0));
}

async function committed(wrapper: Wrapper): Promise<string | undefined> {
  editorOf(wrapper).dispatchEvent(new FocusEvent('blur'));
  await wrapper.vm.$nextTick();
  return wrapper.emitted('update:modelValue')?.at(-1)?.[0] as
    | string
    | undefined;
}

describe('rich-text list editing', () => {
  it('starts a new list item on Enter', async () => {
    const wrapper = mountEditor('<ul><li><p>one</p></li></ul>');
    const editor = editorOf(wrapper);
    await caretAfter(editor, 'one');

    await press(editor, 'Enter');
    await typeText(editor, 'two');

    expect(await committed(wrapper)).toBe(
      '<ul><li><p>one</p></li><li><p>two</p></li></ul>',
    );
  });

  it('leaves the list on Enter in an empty item', async () => {
    const wrapper = mountEditor('<ul><li><p>one</p></li></ul>');
    const editor = editorOf(wrapper);
    await caretAfter(editor, 'one');

    await press(editor, 'Enter');
    await press(editor, 'Enter');
    await typeText(editor, 'after');

    expect(await committed(wrapper)).toBe(
      '<ul><li><p>one</p></li></ul><p>after</p>',
    );
  });

  it('nests an item on Tab and lifts it on Shift-Tab', async () => {
    const wrapper = mountEditor(
      '<ul><li><p>one</p></li><li><p>two</p></li></ul>',
    );
    const editor = editorOf(wrapper);
    await caretAfter(editor, 'two');

    const tab = await press(editor, 'Tab');
    expect(tab.defaultPrevented).toBe(true);
    expect(await committed(wrapper)).toBe(
      '<ul><li><p>one</p><ul><li><p>two</p></li></ul></li></ul>',
    );

    await caretAfter(editor, 'two');
    await press(editor, 'Tab', { shiftKey: true });
    expect(await committed(wrapper)).toBe(
      '<ul><li><p>one</p></li><li><p>two</p></li></ul>',
    );
  });

  it('lets Tab move focus outside a list', async () => {
    const wrapper = mountEditor('<p>text</p>');
    const editor = editorOf(wrapper);
    await caretAfter(editor, 'text');

    const tab = await press(editor, 'Tab');

    expect(tab.defaultPrevented).toBe(false);
  });

  it.each([
    ['-', '<ul><li><p>item</p></li></ul>'],
    ['*', '<ul><li><p>item</p></li></ul>'],
    ['1.', '<ol><li><p>item</p></li></ol>'],
  ])('turns "%s " at the start of a paragraph into a list', async (
    marker,
    expected,
  ) => {
    const wrapper = mountEditor(`<p>${marker}</p>`);
    const editor = editorOf(wrapper);
    await caretAfter(editor, marker);

    await press(editor, ' ');
    await typeText(editor, 'item');

    expect(await committed(wrapper)).toBe(expected);
  });

  it('keeps a marker that is not at the start of a paragraph', async () => {
    const wrapper = mountEditor('<p>a -</p>');
    const editor = editorOf(wrapper);
    await caretAfter(editor, 'a -');

    const space = await press(editor, ' ');

    expect(space.defaultPrevented).toBe(false);
  });
});
