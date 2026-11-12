// Why the queue dropped a refresh without sending it to the end: the
// section that asked for it reads the reason once the command has left the
// queue. Command ids are unique, and each reason is read at most once.

/** The entry's text changed after the person confirmed the refresh. */
export type ProjectOutcome = 'refresh-changed';

const outcomes = new Map<string, ProjectOutcome>();

export function noteProjectOutcome(
  commandId: string,
  outcome: ProjectOutcome,
): void {
  outcomes.set(commandId, outcome);
}

export function takeProjectOutcome(
  commandId: string,
): ProjectOutcome | undefined {
  const outcome = outcomes.get(commandId);
  outcomes.delete(commandId);
  return outcome;
}
