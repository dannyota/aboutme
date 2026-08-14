import { EXTERNAL_REL } from '@aboutme/schema/sanitizer';
import type { Mark, Node as PMNode } from 'prosemirror-model';

const escapeHTML = (value: string): string =>
  value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');

const allowedHref = (value: unknown): value is string =>
  typeof value === 'string' && /^(?:https:|mailto:|tel:)/.test(value);

function serializeMark(mark: Mark, content: string): string {
  switch (mark.type.name) {
    case 'strong':
      return `<strong>${content}</strong>`;
    case 'em':
      return `<em>${content}</em>`;
    case 'underline':
      return `<u>${content}</u>`;
    case 'link': {
      if (!allowedHref(mark.attrs.href)) return content;
      const target = mark.attrs.target === '_blank' ? ' target="_blank"' : '';
      const href = escapeHTML(mark.attrs.href);
      return `<a href="${href}" rel="${EXTERNAL_REL}"${target}>${content}</a>`;
    }
    default:
      return content;
  }
}

function serializeContent(node: PMNode): string {
  let content = '';
  node.forEach((child) => {
    content += serializeNode(child);
  });
  return content;
}

function serializeNode(node: PMNode): string {
  if (node.isText) {
    let content = escapeHTML(node.text ?? '');
    for (const mark of node.marks) content = serializeMark(mark, content);
    return content;
  }

  if (node.type.name === 'doc') {
    const children: PMNode[] = [];
    node.forEach((child) => {
      children.push(child);
    });
    while (
      children.at(-1)?.type.name === 'paragraph'
      && children.at(-1)?.content.size === 0
    ) {
      children.pop();
    }
    return children.map((child) => serializeNode(child)).join('');
  }

  const content = serializeContent(node);
  switch (node.type.name) {
    case 'paragraph':
      return `<p>${content}</p>`;
    case 'hard_break':
      return '<br>';
    case 'ordered_list':
      return `<ol>${content}</ol>`;
    case 'bullet_list':
      return `<ul>${content}</ul>`;
    case 'list_item':
      return `<li>${content}</li>`;
    default:
      return content;
  }
}

export function serializeRichText(node: PMNode): string {
  return serializeNode(node);
}
