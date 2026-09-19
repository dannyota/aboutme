import { chainCommands } from 'prosemirror-commands';
import type { NodeType } from 'prosemirror-model';
import {
  liftListItem,
  sinkListItem,
  splitListItem,
  wrapRangeInList,
} from 'prosemirror-schema-list';
import type { Command, EditorState, Transaction } from 'prosemirror-state';
import type { EditorView } from 'prosemirror-view';

import { richTextSchema } from './schema';

const item = richTextSchema.nodes.list_item;

/**
 * List keys: Enter starts a new item and, in an empty item, leaves the list;
 * Tab nests the item and Shift-Tab lifts it. Outside a list each command
 * declines, so Tab still moves focus.
 */
export const listKeymap: Record<string, Command> = {
  'Enter': chainCommands(splitListItem(item), liftListItem(item)),
  'Tab': sinkListItem(item),
  'Shift-Tab': liftListItem(item),
};

function markerList(text: string): NodeType | undefined {
  if (text === '-' || text === '*') return richTextSchema.nodes.bullet_list;
  if (text === '1.') return richTextSchema.nodes.ordered_list;
  return undefined;
}

/**
 * Typing a space after "-", "*", or "1." at the start of a top-level
 * paragraph replaces the marker with a bullet or ordered list.
 */
export function startListFromMarker(
  state: EditorState,
  dispatch?: (tr: Transaction) => void,
): boolean {
  const { $from, empty } = state.selection;
  if (!empty || $from.parent.type !== richTextSchema.nodes.paragraph) {
    return false;
  }
  if ($from.depth !== 1) return false;
  const listType = markerList(
    $from.parent.textBetween(0, $from.parentOffset),
  );
  if (listType === undefined) return false;

  const tr = state.tr.delete($from.start(), $from.pos);
  const range = tr.doc.resolve($from.start()).blockRange();
  if (range === null || !wrapRangeInList(tr, range, listType)) return false;
  dispatch?.(tr.scrollIntoView());
  return true;
}

/** The same rule for input that arrives without a key event (mobile IMEs). */
export function handleListMarkerInput(
  view: EditorView,
  from: number,
  to: number,
  text: string,
): boolean {
  if (text !== ' ' || from !== to) return false;
  return startListFromMarker(view.state, view.dispatch);
}
