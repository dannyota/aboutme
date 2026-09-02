// @vitest-environment jsdom

import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';

import RichTextEditor from
  '../../app/components/editor/richtext/RichTextEditor.vue';
import {
  parseRichTextHTML,
  richTextSchema,
} from '../../app/components/editor/richtext/schema';
import { serializeRichText } from
  '../../app/components/editor/richtext/serialize';

const unsupportedWrapperCase
  = 'keeps only sanitizer-v1 elements and normalizes unsupported wrappers';
const hostileCorpusCase
  = 'canonicalizes every hostile corpus value through the closed model';

async function selectEditorText(editor: HTMLElement): Promise<void> {
  const text = editor.querySelector('p')?.firstChild;
  if (text === null || text === undefined) {
    throw new Error('Expected rich-text paragraph content.');
  }

  editor.focus();
  const range = document.createRange();
  range.selectNodeContents(text);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
  document.dispatchEvent(new Event('selectionchange'));
  await new Promise((resolve) => setTimeout(resolve, 20));
}

function latestEmission(
  wrapper: ReturnType<typeof mount>,
): string | undefined {
  return wrapper.emitted('update:modelValue')?.at(-1)?.[0] as
    | string
    | undefined;
}

describe('closed rich-text schema', () => {
  it(unsupportedWrapperCase, () => {
    // This fails if an unsupported tag becomes a schema node or text in an
    // unsupported wrapper is discarded instead of normalized to a paragraph.
    expect(Object.keys(richTextSchema.nodes)).toEqual([
      'doc',
      'paragraph',
      'text',
      'hard_break',
      'ordered_list',
      'bullet_list',
      'list_item',
    ]);
    expect(Object.keys(richTextSchema.marks)).toEqual([
      'strong',
      'em',
      'underline',
      'link',
    ]);
    expect(
      serializeRichText(parseRichTextHTML('<h1>x</h1><img src=x><p></p>')),
    ).toBe('<p>x</p>');
  });

  it('preserves an empty paragraph between populated paragraphs', () => {
    expect(
      serializeRichText(parseRichTextHTML('<p>a</p><p></p><p>b</p>')),
    ).toBe('<p>a</p><p></p><p>b</p>');
  });

  it('normalizes one empty paragraph to an empty field value', () => {
    expect(serializeRichText(parseRichTextHTML('<p></p>'))).toBe('');
  });

  it('removes trailing empty paragraphs from populated content', () => {
    expect(serializeRichText(parseRichTextHTML('<p>a</p><p></p>'))).toBe(
      '<p>a</p>',
    );
  });

  it('normalizes all-empty paragraphs to an empty field value', () => {
    expect(serializeRichText(parseRichTextHTML('<p></p><p></p>'))).toBe('');
  });

  it.each([
    [
      '<a href="https://example.com">x</a>',
      '<p><a href="https://example.com" rel="noopener noreferrer">x</a></p>',
    ],
    [
      '<a href="mailto:hello@example.com">x</a>',
      '<p><a href="mailto:hello@example.com" '
      + 'rel="noopener noreferrer">x</a></p>',
    ],
    [
      '<a href="tel:+84123456789">x</a>',
      '<p><a href="tel:+84123456789" rel="noopener noreferrer">x</a></p>',
    ],
    ['<a href="HTTPS://example.com">x</a>', '<p>x</p>'],
    ['<a href="//example.com">x</a>', '<p>x</p>'],
    ['<a href=" javascript:alert(1)">x</a>', '<p>x</p>'],
  ])('admits only lowercase explicit link schemes', (input, output) => {
    expect(serializeRichText(parseRichTextHTML(input))).toBe(output);
  });

  it(hostileCorpusCase, () => {
    for (const { id, payload } of HOSTILE_CORPUS) {
      const output = serializeRichText(parseRichTextHTML(payload));
      expect(output, id).not.toMatch(
        /<(?:img|script|style|table|iframe|svg)\b/i,
      );
      expect(output, id).not.toMatch(/\s(?:class|style|on\w+)=/i);
      expect(output, id).not.toMatch(
        /href="(?:javascript|data|file|vbscript):/i,
      );
    }
  });
});

describe('RichTextEditor', () => {
  it('blocks file paste and drop without emitting', async () => {
    const wrapper = mount(RichTextEditor, { props: { modelValue: '' } });
    const editor = wrapper.get('[contenteditable="true"]');
    const file = new File(['x'], 'x.png', { type: 'image/png' });

    const paste = new Event('paste', { cancelable: true });
    Object.defineProperty(paste, 'clipboardData', {
      value: { files: [file], getData: () => '' },
    });
    editor.element.dispatchEvent(paste);
    await wrapper.vm.$nextTick();

    const drop = new Event('drop', { cancelable: true });
    Object.defineProperty(drop, 'dataTransfer', {
      value: { files: [file] },
    });
    editor.element.dispatchEvent(drop);
    await wrapper.vm.$nextTick();

    expect(paste.defaultPrevented).toBe(true);
    expect(drop.defaultPrevented).toBe(true);
    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
  });

  it('retains plain-text paste and emits once', async () => {
    const wrapper = mount(RichTextEditor, { props: { modelValue: '' } });
    const editor = wrapper.get('[contenteditable="true"]');
    const paste = new Event('paste', { cancelable: true });
    Object.defineProperty(paste, 'clipboardData', {
      value: {
        files: [],
        getData: (kind: string) => (kind === 'text/plain' ? 'plain text' : ''),
      },
    });

    editor.element.dispatchEvent(paste);
    await wrapper.vm.$nextTick();

    expect(paste.defaultPrevented).toBe(true);
    expect(wrapper.emitted('update:modelValue')).toEqual([
      ['<p>plain text</p>'],
    ]);
  });

  it('sanitizes HTML paste before inserting it', async () => {
    const wrapper = mount(RichTextEditor, { props: { modelValue: '' } });
    const editor = wrapper.get('[contenteditable="true"]');
    const paste = new Event('paste', { cancelable: true });
    Object.defineProperty(paste, 'clipboardData', {
      value: {
        files: [],
        getData: (kind: string) =>
          kind === 'text/html'
            ? '<img src=x><p style="color:red">safe</p>'
            : '',
      },
    });

    editor.element.dispatchEvent(paste);
    await wrapper.vm.$nextTick();

    expect(paste.defaultPrevented).toBe(true);
    expect(latestEmission(wrapper)).toBe('<p>safe</p>');
  });

  it('does not emit for focus, blur, or a prop replacement', async () => {
    const wrapper = mount(RichTextEditor, { props: { modelValue: '' } });
    const editor = wrapper.get('[contenteditable="true"]');

    await editor.trigger('focus');
    await editor.trigger('blur');
    await wrapper.setProps({ modelValue: '<p>accepted</p>' });

    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
    expect(editor.html()).toContain('accepted');
  });

  it('labels the editable ProseMirror surface as a multiline textbox', () => {
    const wrapper = mount(RichTextEditor, { props: { modelValue: '' } });
    const editor = wrapper.get('[contenteditable="true"]');

    expect(editor.attributes()).toMatchObject({
      'aria-label': 'Rich text',
      'aria-multiline': 'true',
      'role': 'textbox',
    });
  });

  it('uses an explicit accessible label when supplied', () => {
    const wrapper = mount(RichTextEditor, {
      props: { label: 'Work description', modelValue: '' },
    });
    const editor = wrapper.get('[contenteditable="true"]');
    expect(editor.attributes('aria-label')).toBe(
      'Work description',
    );
  });

  it('emits an empty string when a present value is cleared', async () => {
    const wrapper = mount(RichTextEditor, {
      props: { modelValue: '<p>text</p>' },
      attachTo: document.body,
    });
    const editor = wrapper.get('[contenteditable="true"]');
    await selectEditorText(editor.element);

    await editor.trigger('keydown', { key: 'Backspace' });

    expect(latestEmission(wrapper)).toBe('');
  });

  it('runs every material toolbar command by button and keyboard', async () => {
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
    const rects = vi
      .spyOn(HTMLElement.prototype, 'getClientRects')
      .mockReturnValue([rect] as unknown as DOMRectList);
    const textDescriptor = Object.getOwnPropertyDescriptor(
      Text.prototype,
      'getClientRects',
    );
    Object.defineProperty(Text.prototype, 'getClientRects', {
      configurable: true,
      value: () => [rect],
    });
    const rangeDescriptor = Object.getOwnPropertyDescriptor(
      Range.prototype,
      'getClientRects',
    );
    Object.defineProperty(Range.prototype, 'getClientRects', {
      configurable: true,
      value: () => [rect],
    });
    const prompt = vi
      .spyOn(window, 'prompt')
      .mockReturnValue('https://example.com');
    const scrollBy = vi.spyOn(window, 'scrollBy').mockImplementation(() => {});
    const actions = [
      {
        button: 'Line break',
        expected: '<p><br></p>',
        keyboard: { key: 'Enter', ctrlKey: true },
        modelValue: '<p>text</p>',
      },
      {
        button: 'Bold',
        expected: '<p><strong>text</strong></p>',
        keyboard: { key: 'b', ctrlKey: true },
        modelValue: '<p>text</p>',
      },
      {
        button: 'Italic',
        expected: '<p><em>text</em></p>',
        keyboard: { key: 'i', ctrlKey: true },
        modelValue: '<p>text</p>',
      },
      {
        button: 'Underline',
        expected: '<p><u>text</u></p>',
        keyboard: { key: 'u', ctrlKey: true },
        modelValue: '<p>text</p>',
      },
      {
        button: 'Ordered list',
        expected: '<ol><li><p>text</p></li></ol>',
        keyboard: { key: '8', ctrlKey: true, shiftKey: true },
        modelValue: '<p>text</p>',
      },
      {
        button: 'Bullet list',
        expected: '<ul><li><p>text</p></li></ul>',
        keyboard: { key: '9', ctrlKey: true, shiftKey: true },
        modelValue: '<p>text</p>',
      },
      {
        button: 'Link',
        expected:
          '<p><a href="https://example.com" rel="noopener noreferrer">text</a></p>',
        keyboard: { key: 'k', ctrlKey: true },
        modelValue: '<p>text</p>',
      },
      {
        button: 'Unlink',
        expected: '<p>text</p>',
        keyboard: { key: 'k', ctrlKey: true, shiftKey: true },
        modelValue:
          '<p><a href="https://example.com" rel="noopener noreferrer">text</a></p>',
      },
    ] as const;

    for (const action of actions) {
      for (const trigger of ['button', 'keyboard'] as const) {
        const wrapper = mount(RichTextEditor, {
          attachTo: document.body,
          props: { modelValue: action.modelValue },
        });
        const editor = wrapper.get('[contenteditable="true"]');
        await selectEditorText(editor.element);
        if (trigger === 'button') {
          await wrapper
            .get(`button[aria-label="${action.button}"]`)
            .trigger('click');
        } else {
          await editor.trigger('keydown', action.keyboard);
        }
        expect(latestEmission(wrapper), `${action.button} by ${trigger}`).toBe(
          action.expected,
        );
        wrapper.unmount();
      }
    }

    const paragraph = mount(RichTextEditor, {
      attachTo: document.body,
      props: { modelValue: '<p>text</p>' },
    });
    const paragraphEditor = paragraph.get('[contenteditable="true"]');
    await selectEditorText(paragraphEditor.element);
    await paragraph.get('button[aria-label="Paragraph"]').trigger('click');
    await paragraphEditor.trigger('keydown', {
      altKey: true,
      ctrlKey: true,
      key: '0',
    });
    expect(paragraphEditor.html()).toContain('<p>text</p>');
    expect(latestEmission(paragraph)).toBeUndefined();
    paragraph.unmount();

    expect(prompt).toHaveBeenCalledTimes(2);
    prompt.mockRestore();
    scrollBy.mockRestore();
    rects.mockRestore();
    if (textDescriptor === undefined) {
      delete (Text.prototype as { getClientRects?: unknown }).getClientRects;
    } else {
      Object.defineProperty(Text.prototype, 'getClientRects', textDescriptor);
    }
    if (rangeDescriptor === undefined) {
      delete (Range.prototype as { getClientRects?: unknown }).getClientRects;
    } else {
      Object.defineProperty(Range.prototype, 'getClientRects', rangeDescriptor);
    }
  });
});
