import type { Section } from '@aboutme/schema';

import type { BlockRef } from './paginate';

type BodyField = 'description' | 'text';

const bodyFields: Partial<Record<Section['sectionType'], BodyField>> = {
  work: 'description',
  education: 'description',
  certificate: 'description',
  project: 'description',
  custom: 'description',
  profile: 'text',
};

const blockTag = /<(\/?)(p|ul|ol|li)\b[^>]*>/giu;

export function splitRichTextBlocks(html: string): string[] {
  const blocks: string[] = [];
  const stack: string[] = [];
  let cursor = 0;
  let blockStart = -1;
  let listOpen = '';
  for (const match of html.matchAll(blockTag)) {
    const [tag, closing, rawName] = match;
    const name = rawName!.toLowerCase();
    const at = match.index;
    if (closing === '') {
      if (stack.length === 0) {
        if (html.slice(cursor, at).trim() !== '') return [html];
        if (name === 'ul') {
          listOpen = tag;
          cursor = at + tag.length;
        } else if (name === 'p' || name === 'ol') blockStart = at;
        else return [html];
      } else if (stack.length === 1 && stack[0] === 'ul' && name === 'li') {
        if (html.slice(cursor, at).trim() !== '') return [html];
        blockStart = at;
      }
      stack.push(name);
      continue;
    }
    if (stack.at(-1) !== name) return [html];
    stack.pop();
    const end = at + tag.length;
    if (stack.length === 0 && (name === 'p' || name === 'ol')) {
      blocks.push(html.slice(blockStart, end));
      cursor = end;
    } else if (stack.length === 1 && stack[0] === 'ul' && name === 'li') {
      blocks.push(`${listOpen}${html.slice(blockStart, end)}</ul>`);
      cursor = end;
    } else if (stack.length === 0 && name === 'ul') {
      if (html.slice(cursor, at).trim() !== '') return [html];
      cursor = end;
    }
  }
  const trailing = html.slice(cursor).trim();
  if (stack.length !== 0 || blocks.length < 2 || trailing !== '') {
    return [html];
  }
  return blocks;
}

const bodyOf = (
  section: Section,
  entryIndex: number,
): string | undefined => {
  const field = bodyFields[section.sectionType];
  const entry = section.entries[entryIndex] as
    | Record<string, unknown>
    | undefined;
  const value = field === undefined ? undefined : entry?.[field];
  return typeof value === 'string' ? value : undefined;
};

export function entryBlocks(
  sectionKey: string,
  section: Section,
  entryIndex: number,
  column: BlockRef['column'],
): BlockRef[] {
  const body = bodyOf(section, entryIndex);
  const parts = body === undefined ? 1 : splitRichTextBlocks(body).length;
  const base: BlockRef = { sectionKey, kind: 'entry', entryIndex, column };
  if (parts < 2) return [base];
  return Array.from({ length: parts }, (_, part) => ({ ...base, part }));
}

export function entryPartSection(
  section: Section,
  entryIndex: number,
  part: number,
): Section {
  const clone = structuredClone(section);
  clone.entries.splice(entryIndex + 1);
  clone.entries.splice(0, entryIndex);
  const field = bodyFields[section.sectionType];
  const body = bodyOf(section, entryIndex);
  const pieces = body === undefined ? [] : splitRichTextBlocks(body);
  const piece = pieces[part];
  if (field !== undefined && piece !== undefined && pieces.length > 1) {
    const entry = clone.entries[0] as unknown as Record<string, unknown>;
    entry[field] = piece;
  }
  return clone;
}
