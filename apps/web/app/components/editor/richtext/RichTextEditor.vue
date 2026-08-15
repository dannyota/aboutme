<script setup lang="ts">
import { baseKeymap, setBlockType, toggleMark } from 'prosemirror-commands';
import { history } from 'prosemirror-history';
import { keymap } from 'prosemirror-keymap';
import { wrapInList } from 'prosemirror-schema-list';
import { EditorState, type Command, type Transaction } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { parseRichTextHTML, richTextSchema } from './schema';
import { serializeRichText } from './serialize';

const props = withDefaults(
  defineProps<{ readonly label?: string; readonly modelValue: string }>(),
  { label: 'Rich text' },
);
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();

const editorRoot = ref<HTMLElement>();
let view: EditorView | undefined;
let lastInput = props.modelValue;

function hasFiles(files: FileList | readonly File[] | undefined): boolean {
  return files !== undefined && files.length > 0;
}

function insertHardBreak(
  state: EditorState,
  dispatch?: (tr: Transaction) => void,
): boolean {
  if (dispatch === undefined) return true;
  dispatch(
    state.tr.replaceSelectionWith(richTextSchema.nodes.hard_break.create()),
  );
  return true;
}

function applyLink(editor: EditorView): boolean {
  const href = window.prompt('Link URL');
  if (href === null || !/^(?:https:|mailto:|tel:)/.test(href)) return false;
  return toggleMark(richTextSchema.marks.link, {
    href,
    rel: 'noopener noreferrer',
    target: null,
  })(editor.state, editor.dispatch, editor);
}

function unlink(editor: EditorView): boolean {
  return toggleMark(richTextSchema.marks.link)(
    editor.state,
    editor.dispatch,
    editor,
  );
}

function createState(
  document = parseRichTextHTML(props.modelValue),
): EditorState {
  return EditorState.create({
    doc: document,
    plugins: [
      history(),
      keymap({
        'Mod-Alt-0': setBlockType(richTextSchema.nodes.paragraph),
        'Mod-b': toggleMark(richTextSchema.marks.strong),
        'Mod-i': toggleMark(richTextSchema.marks.em),
        'Mod-u': toggleMark(richTextSchema.marks.underline),
        'Mod-Shift-8': wrapInList(richTextSchema.nodes.ordered_list),
        'Mod-Shift-9': wrapInList(richTextSchema.nodes.bullet_list),
        'Mod-Enter': insertHardBreak,
        'Mod-k': () => view !== undefined && applyLink(view),
        'Mod-Shift-k': () => view !== undefined && unlink(view),
      }),
      keymap(baseKeymap),
    ],
    schema: richTextSchema,
  });
}

function dispatchTransaction(transaction: Transaction): void {
  if (view === undefined) return;
  const nextState = view.state.apply(transaction);
  view.updateState(nextState);
  if (!transaction.docChanged) return;

  const output = serializeRichText(nextState.doc);
  if (output === lastInput) return;
  lastInput = output;
  emit('update:modelValue', output);
}

function run(command: Command): void {
  if (view === undefined) return;
  command(view.state, view.dispatch, view);
  view.focus();
}

function runLink(): void {
  if (view === undefined) return;
  applyLink(view);
  view.focus();
}

function runUnlink(): void {
  if (view === undefined) return;
  unlink(view);
  view.focus();
}

function blockDroppedFiles(event: DragEvent): void {
  if (!hasFiles(event.dataTransfer?.files)) return;
  event.preventDefault();
  event.stopImmediatePropagation();
}

onMounted(() => {
  if (editorRoot.value === undefined) return;
  view = new EditorView(editorRoot.value, {
    attributes: {
      'aria-label': props.label,
      'aria-multiline': 'true',
      'role': 'textbox',
    },
    dispatchTransaction,
    handleDrop: (_editor, event) => {
      if (!hasFiles(event.dataTransfer?.files)) return false;
      event.preventDefault();
      return true;
    },
    handlePaste: (editor, event) => {
      if (hasFiles(event.clipboardData?.files)) {
        event.preventDefault();
        return true;
      }

      const html = event.clipboardData?.getData('text/html') ?? '';
      if (html !== '') {
        event.preventDefault();
        editor.dispatch(
          editor.state.tr.replaceSelection(parseRichTextHTML(html).slice(0)),
        );
        return true;
      }

      const plainText = event.clipboardData?.getData('text/plain') ?? '';
      if (plainText === '') return false;
      event.preventDefault();
      editor.dispatch(editor.state.tr.insertText(plainText));
      return true;
    },
    state: createState(),
  });
});

onBeforeUnmount(() => view?.destroy());

watch(
  () => props.modelValue,
  (modelValue) => {
    if (view === undefined || modelValue === lastInput) return;
    lastInput = modelValue;
    view.updateState(createState(parseRichTextHTML(modelValue)));
  },
);
</script>

<template>
  <div
    class="rich-text-editor"
    @drop.capture="blockDroppedFiles"
  >
    <div
      aria-label="Rich-text controls"
      role="toolbar"
    >
      <button
        aria-keyshortcuts="Control+Alt+0"
        aria-label="Paragraph"
        type="button"
        @click="run(setBlockType(richTextSchema.nodes.paragraph))"
      >
        Paragraph
      </button>
      <button
        aria-keyshortcuts="Control+Enter"
        aria-label="Line break"
        type="button"
        @click="run(insertHardBreak)"
      >
        Line break
      </button>
      <button
        aria-keyshortcuts="Control+B"
        aria-label="Bold"
        type="button"
        @click="run(toggleMark(richTextSchema.marks.strong))"
      >
        Bold
      </button>
      <button
        aria-keyshortcuts="Control+I"
        aria-label="Italic"
        type="button"
        @click="run(toggleMark(richTextSchema.marks.em))"
      >
        Italic
      </button>
      <button
        aria-keyshortcuts="Control+U"
        aria-label="Underline"
        type="button"
        @click="run(toggleMark(richTextSchema.marks.underline))"
      >
        Underline
      </button>
      <button
        aria-keyshortcuts="Control+Shift+8"
        aria-label="Ordered list"
        type="button"
        @click="run(wrapInList(richTextSchema.nodes.ordered_list))"
      >
        Ordered list
      </button>
      <button
        aria-keyshortcuts="Control+Shift+9"
        aria-label="Bullet list"
        type="button"
        @click="run(wrapInList(richTextSchema.nodes.bullet_list))"
      >
        Bullet list
      </button>
      <button
        aria-keyshortcuts="Control+K"
        aria-label="Link"
        type="button"
        @click="runLink"
      >
        Link
      </button>
      <button
        aria-keyshortcuts="Control+Shift+K"
        aria-label="Unlink"
        type="button"
        @click="runUnlink"
      >
        Unlink
      </button>
    </div>
    <div ref="editorRoot" />
  </div>
</template>
