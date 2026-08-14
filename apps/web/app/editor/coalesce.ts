import type { AtomicEditorCommand } from './commands';

export function coalescePending(
  pending: readonly AtomicEditorCommand[],
  next: AtomicEditorCommand,
): readonly AtomicEditorCommand[] {
  const output = [...pending];
  const previous = output.at(-1);
  if (previous !== undefined && isCoalescible(previous, next)) {
    output[output.length - 1] = Object.freeze({
      ...next,
      id: previous.id,
      sequence: previous.sequence,
      base: previous.base,
      dependencyIds: previous.dependencyIds,
    }) as AtomicEditorCommand;
  } else {
    output.push(next);
  }
  return output;
}

function isCoalescible(
  previous: AtomicEditorCommand,
  next: AtomicEditorCommand,
): boolean {
  return (
    previous.kind === next.kind
    && previous.targetKey === next.targetKey
    && isValueCommand(previous)
    && isValueCommand(next)
  );
}

function isValueCommand(command: AtomicEditorCommand): boolean {
  switch (command.kind) {
    case 'metadataField':
    case 'personalField':
    case 'entryField':
    case 'entryReorder':
    case 'sectionMetadata':
    case 'photoCrop':
      return true;
    case 'customization':
      return command.deltas.length === 1;
    case 'entryUpsert':
    case 'entryDelete':
    case 'structure':
    case 'photoUpload':
    case 'photoDelete':
    case 'resumeDelete':
      return false;
    default:
      return assertNever(command);
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled command: ${String(value)}`);
}
