// View rules for the verify page (docs/design/deployment-transparency/
// visual.md): chain step chips and connectors, component rows, the command
// selector, and the verify commands. Pure functions of the page state, so the
// components only render.
import {
  chainRelease,
  type ChainRelease,
  type Component,
  type ComponentName,
  componentNames,
  type DeploymentDocument,
  type Failure,
  type PageState,
  type RunningImage,
  servingComponents,
} from './deploymentDocument';

export type ChipKind
  = | 'match' | 'updating' | 'not_rechecked' | 'not_verified' | 'failed';
export type ConnectorKind = 'agree' | 'updating' | 'unknown' | 'disagree';

export interface ChainView {
  /** Source, Build, Image, Running. */
  readonly chips: readonly [ChipKind, ChipKind, ChipKind, ChipKind];
  /** Source→Build, Build→Image, Image→Running. */
  readonly connectors: readonly [ConnectorKind, ConnectorKind, ConnectorKind];
  /** The failure a `failed` chip names. */
  readonly failure: Failure | null;
  readonly release: ChainRelease | null;
  /** Replicas across the serving components. */
  readonly replicas: number;
  /** When the shown version started running everywhere it runs. */
  readonly runningSince: string | null;
}

type Four = readonly [ChipKind, ChipKind, ChipKind, ChipKind];
type Three = readonly [ConnectorKind, ConnectorKind, ConnectorKind];

function steps(state: PageState, hasRelease: boolean): {
  chips: Four;
  connectors: Three;
  failure: Failure | null;
} {
  const none: Four = [
    'not_verified', 'not_verified', 'not_verified', 'not_verified'];
  const unknown: Three = ['unknown', 'unknown', 'unknown'];
  switch (state.kind) {
    case 'verified':
      return {
        chips: ['match', 'match', 'match', 'match'],
        connectors: ['agree', 'agree', 'agree'],
        failure: null,
      };
    case 'rolling_out':
      return {
        chips: ['match', 'match', 'match', 'updating'],
        connectors: ['agree', 'agree', 'updating'],
        failure: null,
      };
    case 'unverified':
      return hasRelease
        ? {
            chips: ['match', 'match', 'not_verified', 'not_verified'],
            connectors: ['agree', 'unknown', 'unknown'],
            failure: null,
          }
        : { chips: none, connectors: unknown, failure: null };
    case 'mismatch': {
      const { cause } = state;
      if (cause === null) {
        return { chips: none, connectors: unknown, failure: null };
      }
      const before: ChipKind = hasRelease ? 'match' : 'not_verified';
      const link: ConnectorKind = hasRelease ? 'agree' : 'unknown';
      return cause.reason === 'missing'
        ? {
            chips: [before, before, before, 'failed'],
            connectors: [link, link, 'disagree'],
            failure: cause,
          }
        : {
            chips: [before, before, 'failed', 'not_verified'],
            connectors: [link, 'disagree', 'unknown'],
            failure: cause,
          };
    }
    case 'stale':
      return {
        chips: [
          'not_rechecked', 'not_rechecked', 'not_rechecked', 'not_rechecked'],
        connectors: unknown,
        failure: null,
      };
    default:
      return { chips: none, connectors: unknown, failure: null };
  }
}

function servingImages(document: DeploymentDocument): RunningImage[] {
  return document.components
    .filter(({ name }) => servingComponents.includes(name))
    .flatMap(({ runningImages }) => runningImages);
}

/** The chain for a state that has a document; null for the others. */
export function chainView(state: PageState): ChainView | null {
  if (!('document' in state)) return null;
  const { document } = state;
  const release = chainRelease(document);
  const images = servingImages(document);
  const shown = release === null
    ? []
    : images.filter(({ version }) => version === release.version);
  const runningSince = document.release?.deployedAt
    ?? shown.map(({ runningSince: since }) => since)
      .sort((left, right) => Date.parse(right) - Date.parse(left))[0]
      ?? null;
  return {
    ...steps(state, release !== null),
    release,
    replicas: images.reduce((sum, { replicas }) => sum + replicas, 0),
    runningSince,
  };
}

/** The GitHub Actions run id in a build link. */
export function buildRunId(link: string | null): string | null {
  return link?.match(/\/actions\/runs\/(\d+)\//u)?.[1] ?? null;
}

export interface ImageLine {
  readonly image: RunningImage;
  /** Set during a rollout of this component. */
  readonly age: 'older' | 'newest' | null;
  readonly failing: boolean;
}

export interface ComponentRow {
  readonly name: ComponentName;
  readonly lines: readonly ImageLine[];
  readonly rolling: boolean;
  readonly failing: boolean;
}

/**
 * One row per serving component, present or not, then `maintenance` while it
 * runs. A serving component with no running image keeps its row so the
 * failure has a place to show.
 */
export function componentRows(document: DeploymentDocument): ComponentRow[] {
  const byName = new Map<ComponentName, Component>(
    document.components.map((entry) => [entry.name, entry]));
  return componentNames.flatMap((name) => {
    const images = byName.get(name)?.runningImages ?? [];
    if (!servingComponents.includes(name) && images.length === 0) return [];
    const rolling = images.length > 1;
    const lines = images.map((image, index): ImageLine => ({
      image,
      age: rolling ? (index === images.length - 1 ? 'newest' : 'older') : null,
      failing: image.signature === 'not_found'
        || image.signature === 'invalid',
    }));
    return [{
      name,
      lines,
      rolling,
      failing: images.length === 0 || lines.some(({ failing }) => failing),
    }];
  });
}

export interface CommandTarget {
  readonly key: string;
  readonly component: ComponentName;
  /** The version tag the selector shows when a component runs two images. */
  readonly label: string;
  readonly image: RunningImage;
}

export function commandTargets(
  document: DeploymentDocument,
): CommandTarget[] {
  return componentRows(document).flatMap(({ name, lines }) =>
    lines.map(({ image }) => ({
      key: `${name}@${image.digest}`,
      component: name,
      label: lines.length > 1 ? `${name} ${image.version ?? '?'}` : name,
      image,
    })));
}

/**
 * The selector starts on `server`, or on the failing component in the
 * mismatch state; a component in rollout starts on its newest image.
 */
export function defaultTarget(
  state: PageState,
  targets: readonly CommandTarget[],
): CommandTarget | null {
  const failing = state.kind === 'mismatch' ? state.cause : null;
  if (failing !== null && failing.reason !== 'missing') {
    const bad = targets.find(({ component, image }) =>
      component === failing.component
      && (image.signature === 'not_found' || image.signature === 'invalid'));
    if (bad !== undefined) return bad;
  }
  const server = targets.filter(({ component }) => component === 'server');
  return server[server.length - 1] ?? targets[0] ?? null;
}

/** One piece of a command; `live` pieces take the highlight. */
export interface CommandPart {
  readonly text: string;
  readonly kind: 'plain' | 'live' | 'placeholder';
}

export interface Command {
  readonly lines: readonly (readonly CommandPart[])[];
  /** What the copy button writes: the lines, without prompts. */
  readonly copy: string;
}

function command(lines: readonly (readonly CommandPart[])[]): Command {
  return {
    lines,
    copy: lines.map((line) => line.map(({ text }) => text).join('')).join('\n'),
  };
}

const plain = (text: string): CommandPart => ({ text, kind: 'plain' });

/** Step 1 (verification.md, "Verify it yourself"); it has no live values. */
export const readDigestsCommand = command([
  [plain('curl -fsS https://aboutme.vn/.well-known/deployment.json \\')],
  [plain('  | jq -r \'.components[] | .name as $n | .running_images[] '
    + '| "\\($n) \\(.digest) \\(.version)"\'')],
]);

/** Step 2 for one image, or with placeholders when there is none. */
export function verifyImageCommand(target: CommandTarget | null): Command {
  const repository = target === null
    ? plain('server')
    : {
        // Maintenance runs the Caddy image (document.md, component mapping).
        text: target.component === 'maintenance' ? 'caddy' : target.component,
        kind: 'live' as const,
      };
  const digest: CommandPart = target === null
    ? { text: '<digest>', kind: 'placeholder' }
    : { text: target.image.digest.slice('sha256:'.length), kind: 'live' };
  const version: CommandPart = target?.image.version
    ? { text: target.image.version, kind: 'live' }
    : { text: '<version>', kind: 'placeholder' };
  return command([
    [plain('gh attestation verify \\')],
    [plain('  oci://ghcr.io/dannyota/aboutme-'), repository,
      plain('@sha256:'), digest, plain(' \\')],
    [plain('  --repo dannyota/aboutme \\')],
    [plain('  --signer-workflow '
      + 'dannyota/aboutme/.github/workflows/release-images.yml \\')],
    [plain('  --source-ref refs/tags/'), version, plain(' \\')],
    [plain('  --deny-self-hosted-runners')],
  ]);
}

/** "sha256:" plus the first 8 and last 4 hex characters (page.md). */
export function shortDigest(digest: string): string {
  const hex = digest.slice('sha256:'.length);
  return `sha256:${hex.slice(0, 8)}…${hex.slice(-4)}`;
}

export function shortCommit(commit: string): string {
  return commit.slice(0, 7);
}
