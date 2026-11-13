import {
  chainRelease,
  type ChainRelease,
  type DeploymentComponent,
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
export type ReportingPlatform = 'aws-ecs' | 'kubernetes' | 'host' | 'unknown';

export function reportingPlatform(
  document: DeploymentDocument | null,
): ReportingPlatform {
  if (document?.orchestrator === 'podman') return 'host';
  if (document?.orchestrator === 'kubernetes') return 'kubernetes';
  if (document?.provider === 'aws' && document.orchestrator === 'ecs') {
    return 'aws-ecs';
  }
  return 'unknown';
}

export interface ChainView {
  readonly chips: readonly [ChipKind, ChipKind, ChipKind, ChipKind];
  readonly connectors: readonly [ConnectorKind, ConnectorKind, ConnectorKind];
  readonly failure: Failure | null;
  readonly release: ChainRelease | null;
  readonly replicas: number;
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

export function chainView(state: PageState): ChainView | null {
  if (!('document' in state)) return null;
  const { document } = state;
  const release = chainRelease(document);
  const images = servingImages(document);
  const shown = release === null
    ? images
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

export function buildRunId(link: string | null): string | null {
  return link?.match(/\/actions\/runs\/(\d+)\//u)?.[1] ?? null;
}

interface ImageLine {
  readonly image: RunningImage;
  readonly age: 'older' | 'newest' | null;
  readonly failing: boolean;
}

export interface ComponentRow {
  readonly name: ComponentName;
  readonly lines: readonly ImageLine[];
  readonly rolling: boolean;
  readonly failing: boolean;
}

export function componentRows(document: DeploymentDocument): ComponentRow[] {
  const byName = new Map<ComponentName, DeploymentComponent>(
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

export interface CommandPart {
  readonly text: string;
  readonly kind: 'plain' | 'live' | 'placeholder';
}

export interface Command {
  readonly lines: readonly (readonly CommandPart[])[];
  readonly copy: string;
}

function command(lines: readonly (readonly CommandPart[])[]): Command {
  return {
    lines,
    copy: lines.map((line) => line.map(({ text }) => text).join('')).join('\n'),
  };
}

const plain = (text: string): CommandPart => ({ text, kind: 'plain' });

export const readDigestsCommand = command([
  [plain('curl -fsS https://aboutme.vn/.well-known/deployment.json \\')],
  [plain('  | jq -r \'.components[] | .name as $n | .running_images[] '
    + '| "\\($n) \\(.digest) \\(.version)"\'')],
]);

export function verifyImageCommand(target: CommandTarget | null): Command {
  const repository = target === null
    ? plain('server')
    : {
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

export function shortDigest(digest: string): string {
  const hex = digest.slice('sha256:'.length);
  return `sha256:${hex.slice(0, 8)}…${hex.slice(-4)}`;
}

export function shortCommit(commit: string): string {
  return commit.slice(0, 7);
}
