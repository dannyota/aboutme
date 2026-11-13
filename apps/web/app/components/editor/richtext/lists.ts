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

// Mobile IMEs type without key events; ProseMirror calls handleTextInput.
export function handleListMarkerInput(
  view: EditorView,
  from: number,
  to: number,
  text: string,
): boolean {
  if (text !== ' ' || from !== to) return false;
  return startListFromMarker(view.state, view.dispatch);
}
