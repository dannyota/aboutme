import {
  ALLOWED_ATTRIBUTES,
  ALLOWED_TAGS,
  ALLOWED_URL_SCHEMES,
  EXTERNAL_REL,
} from '@aboutme/schema/sanitizer-policy';
import { DOMParser as PMDOMParser, Schema } from 'prosemirror-model';

import { sanitizeRichText } from '../../../utils/sanitizeRichText';

const allowedTags = new Set(ALLOWED_TAGS);
const allowedLinkAttributes = new Set(ALLOWED_ATTRIBUTES.a);
const allowedSchemes = new Set(ALLOWED_URL_SCHEMES);

function sanitizerTag(tag: string): string {
  if (!allowedTags.has(tag)) {
    throw new Error(`Rich-text tag is outside sanitizer v1: ${tag}`);
  }
  return tag;
}

function allowedHref(value: string): boolean {
  const match = /^([a-z][a-z0-9+.-]*):/.exec(value);
  const scheme = match?.[1];
  return scheme !== undefined && allowedSchemes.has(scheme);
}

function linkAttributes(
  element: HTMLElement,
): false | Record<string, string | null> {
  const href = element.getAttribute('href');
  if (
    href === null
    || !allowedLinkAttributes.has('href')
    || !allowedHref(href)
  ) {
    return false;
  }

  return {
    href,
    rel: EXTERNAL_REL,
    target:
      allowedLinkAttributes.has('target')
      && element.getAttribute('target') === '_blank'
        ? '_blank'
        : null,
  };
}

export const richTextSchema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    paragraph: {
      content: 'inline*',
      group: 'block',
      parseDOM: [{ tag: sanitizerTag('p') }],
      toDOM: () => ['p', 0],
    },
    text: { group: 'inline' },
    hard_break: {
      group: 'inline',
      inline: true,
      parseDOM: [{ tag: sanitizerTag('br') }],
      selectable: false,
      toDOM: () => ['br'],
    },
    ordered_list: {
      content: 'list_item+',
      group: 'block',
      parseDOM: [{ tag: sanitizerTag('ol') }],
      toDOM: () => ['ol', 0],
    },
    bullet_list: {
      content: 'list_item+',
      group: 'block',
      parseDOM: [{ tag: sanitizerTag('ul') }],
      toDOM: () => ['ul', 0],
    },
    list_item: {
      content: 'paragraph block*',
      parseDOM: [{ tag: sanitizerTag('li') }],
      toDOM: () => ['li', 0],
    },
  },
  marks: {
    strong: {
      parseDOM: [{ tag: sanitizerTag('strong') }],
      toDOM: () => ['strong', 0],
    },
    em: {
      parseDOM: [{ tag: sanitizerTag('em') }],
      toDOM: () => ['em', 0],
    },
    underline: {
      parseDOM: [{ tag: sanitizerTag('u') }],
      toDOM: () => ['u', 0],
    },
    link: {
      attrs: {
        href: {},
        rel: { default: EXTERNAL_REL },
        target: { default: null },
      },
      inclusive: false,
      parseDOM: [
        {
          getAttrs: (element) => linkAttributes(element as HTMLElement),
          tag: `${sanitizerTag('a')}[href]`,
        },
      ],
      toDOM: (mark) => [
        'a',
        {
          href: mark.attrs.href,
          rel: EXTERNAL_REL,
          ...(mark.attrs.target === '_blank' ? { target: '_blank' } : {}),
        },
        0,
      ],
    },
  },
});

export function parseRichTextHTML(html: string) {
  const sanitized = sanitizeRichText(html);
  const fragment = new DOMParser().parseFromString(sanitized, 'text/html');
  return PMDOMParser.fromSchema(richTextSchema).parse(fragment.body);
}
