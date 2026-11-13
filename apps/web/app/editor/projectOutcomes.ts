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
