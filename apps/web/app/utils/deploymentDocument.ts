// Reads /.well-known/deployment.json, version 1
// (docs/design/deployment-transparency/document.md), and decides what the
// verify page shows (docs/design/deployment-transparency/page.md#states).
// Decoding is closed: every value the page shows or puts in a command must
// match its field pattern, or the whole document is refused. The page never
// trusts the observer's summary; it recomputes it from the components.

export const deploymentDocumentPath = '/.well-known/deployment.json';
export const knownSchemaVersion = 1;
/** The observer refuses to write more; the page refuses to read more. */
export const maxDocumentBytes = 64 * 1024;
/** Stale after this long whatever `stale_after` says (README, staleness). */
export const maxDocumentAgeMs = 600_000;

export const componentNames = [
  'server', 'web', 'caddy', 'maintenance',
] as const;
export type ComponentName = (typeof componentNames)[number];
/** The components every release runs; `maintenance` runs only on demand. */
export const servingComponents: readonly ComponentName[] = [
  'server', 'web', 'caddy',
];

export const componentImages: Readonly<Record<ComponentName, string>> = {
  server: 'ghcr.io/dannyota/aboutme-server',
  web: 'ghcr.io/dannyota/aboutme-web',
  caddy: 'ghcr.io/dannyota/aboutme-caddy',
  maintenance: 'ghcr.io/dannyota/aboutme-caddy',
};

/** `unknown` is any status this page does not know; it is never verified. */
export type CheckStatus
  = | 'verified' | 'not_found' | 'invalid' | 'unchecked' | 'unknown';
export type Summary = 'verified' | 'rolling_out' | 'unverified' | 'mismatch';

export interface ImageLinks {
  readonly commit: string | null;
  readonly release: string | null;
  readonly build: string | null;
  readonly provenance: string;
  readonly sbom: string | null;
  readonly transparencyLog: string | null;
}

export interface RunningImage {
  readonly digest: string;
  readonly replicas: number;
  readonly runningSince: string;
  /** Null unless the signature is verified, whatever the document says. */
  readonly version: string | null;
  readonly commit: string | null;
  readonly signature: CheckStatus;
  readonly sbom: CheckStatus;
  readonly links: ImageLinks;
}

export interface Component {
  readonly name: ComponentName;
  readonly image: string;
  /** Oldest first, by `running_since`. */
  readonly runningImages: readonly RunningImage[];
}

export interface Release {
  readonly version: string;
  readonly commit: string;
  readonly deployedAt: string;
}

export interface DeploymentDocument {
  readonly provider: 'aws' | 'greennode';
  readonly region: string;
  readonly observedAt: string;
  readonly staleAfter: string;
  /** The observer's value, kept raw so an unknown one counts as disagreeing. */
  readonly summary: string;
  readonly release: Release | null;
  readonly components: readonly Component[];
}

export type DecodeResult
  = | { readonly kind: 'document'; readonly document: DeploymentDocument }
    | { readonly kind: 'outdated' }
    | { readonly kind: 'invalid' };

const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const VERSION = /^v[0-9]+\.[0-9]+\.[0-9]+$/u;
const COMMIT = /^[0-9a-f]{40}$/u;
const TIMESTAMP = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/u;
const REGION = /^(?:ap-southeast-1|[A-Z]{2,4}[0-9]{2})$/u;
const REPO = 'https://github\\.com/dannyota/aboutme';
const SEMVER = 'v[0-9]+\\.[0-9]+\\.[0-9]+';
const LINK_PATTERNS = {
  commit: new RegExp(`^${REPO}/commit/[0-9a-f]{40}$`, 'u'),
  release: new RegExp(`^${REPO}/releases/tag/${SEMVER}$`, 'u'),
  build: new RegExp(
    `^${REPO}/actions/runs/[0-9]{1,20}/attempts/[0-9]{1,4}$`, 'u'),
  provenance: new RegExp(
    '^https://api\\.github\\.com/repos/dannyota/aboutme/attestations/'
    + 'sha256:[0-9a-f]{64}\\?predicate_type=provenance$', 'u'),
  sbom: new RegExp(
    `^${REPO}/releases/download/${SEMVER}/aboutme-(?:server|web|caddy)`
    + '\\.spdx\\.json$', 'u'),
  transparencyLog: /^https:\/\/search\.sigstore\.dev\/\?logIndex=[0-9]{1,20}$/u,
} as const;

class Refused extends Error {}

function refuse(): never {
  throw new Refused();
}

function record(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    refuse();
  }
  return value as Record<string, unknown>;
}

function text(value: unknown, pattern: RegExp): string {
  if (typeof value !== 'string' || !pattern.test(value)) refuse();
  return value;
}

function optionalText(value: unknown, pattern: RegExp): string | null {
  return value === null ? null : text(value, pattern);
}

function timestamp(value: unknown): string {
  const stamp = text(value, TIMESTAMP);
  if (Number.isNaN(Date.parse(stamp))) refuse();
  return stamp;
}

function constant(value: unknown, expected: string): void {
  if (value !== expected) refuse();
}

function status(value: unknown): CheckStatus {
  if (typeof value !== 'string') refuse();
  return value === 'verified' || value === 'not_found'
    || value === 'invalid' || value === 'unchecked'
    ? value
    : 'unknown';
}

function links(value: unknown, verified: boolean): ImageLinks {
  const raw = record(value);
  const pick = (key: Exclude<keyof ImageLinks, 'provenance'>) => {
    const decoded = optionalText(
      raw[key === 'transparencyLog' ? 'transparency_log' : key],
      LINK_PATTERNS[key],
    );
    return verified ? decoded : null;
  };
  return {
    commit: pick('commit'),
    release: pick('release'),
    build: pick('build'),
    provenance: text(raw.provenance, LINK_PATTERNS.provenance),
    sbom: pick('sbom'),
    transparencyLog: pick('transparencyLog'),
  };
}

function runningImage(value: unknown): RunningImage {
  const raw = record(value);
  const replicas = raw.replicas;
  if (
    typeof replicas !== 'number' || !Number.isInteger(replicas)
    || replicas < 1 || replicas > 64
  ) {
    refuse();
  }
  const signature = status(record(raw.signature).status);
  const sbom = status(record(raw.sbom).status);
  const verified = signature === 'verified';
  const version = optionalText(raw.version, VERSION);
  const commit = optionalText(raw.commit, COMMIT);
  // A verified signature always names its version and commit.
  if (verified && (version === null || commit === null)) refuse();
  return {
    digest: text(raw.digest, DIGEST),
    replicas,
    runningSince: timestamp(raw.running_since),
    version: verified ? version : null,
    commit: verified ? commit : null,
    signature,
    sbom,
    links: links(raw.links, verified),
  };
}

function component(value: unknown): Component {
  const raw = record(value);
  const name = raw.name;
  if (!componentNames.includes(name as ComponentName)) refuse();
  const componentName = name as ComponentName;
  constant(raw.image, componentImages[componentName]);
  if (!Array.isArray(raw.running_images) || raw.running_images.length > 8) {
    refuse();
  }
  const images = raw.running_images.map(runningImage);
  if (new Set(images.map(({ digest }) => digest)).size !== images.length) {
    refuse();
  }
  // Stable sort: equal start times keep the document's order.
  images.sort((left, right) =>
    Date.parse(left.runningSince) - Date.parse(right.runningSince));
  return { name: componentName, image: componentImages[componentName],
    runningImages: images };
}

function release(value: unknown): Release | null {
  if (value === null) return null;
  const raw = record(value);
  return {
    version: text(raw.version, VERSION),
    commit: text(raw.commit, COMMIT),
    deployedAt: timestamp(raw.deployed_at),
  };
}

/** Decodes a parsed body, ignoring unknown fields (document.md, versions). */
export function decodeDeploymentDocument(value: unknown): DecodeResult {
  try {
    const raw = record(value);
    const version = raw.schema_version;
    if (typeof version !== 'number' || !Number.isInteger(version)) refuse();
    if (version > knownSchemaVersion) return { kind: 'outdated' };
    if (version !== knownSchemaVersion) refuse();
    constant(raw.project, 'aboutme');
    constant(raw.environment, 'production');
    constant(raw.site, 'https://aboutme.vn');
    constant(raw.source_repository, 'https://github.com/dannyota/aboutme');
    const platform = record(raw.platform);
    const provider = platform.provider;
    if (provider !== 'aws' && provider !== 'greennode') refuse();
    if (platform.orchestrator !== 'ecs'
      && platform.orchestrator !== 'kubernetes') refuse();
    if (typeof raw.summary !== 'string') refuse();
    if (!Array.isArray(raw.components) || raw.components.length > 4) refuse();
    const components = raw.components.map(component);
    const order = components.map(({ name }) => componentNames.indexOf(name));
    if (order.some((index, at) => at > 0 && index <= order[at - 1]!)) {
      refuse();
    }
    return {
      kind: 'document',
      document: {
        provider,
        region: text(platform.region, REGION),
        observedAt: timestamp(raw.observed_at),
        staleAfter: timestamp(raw.stale_after),
        summary: raw.summary,
        release: release(raw.release),
        components,
      },
    };
  } catch (error) {
    if (error instanceof Refused) return { kind: 'invalid' };
    throw error;
  }
}

function imagesOf(document: DeploymentDocument): RunningImage[] {
  return document.components.flatMap(({ runningImages }) => runningImages);
}

function servingComponent(
  document: DeploymentDocument,
  name: ComponentName,
): Component | undefined {
  return document.components.find((entry) => entry.name === name);
}

const failed = (image: RunningImage) =>
  image.signature === 'not_found' || image.signature === 'invalid';

/** The summary rules of document.md, "Summary and release". */
export function recomputeSummary(document: DeploymentDocument): Summary {
  const images = imagesOf(document);
  const missing = servingComponents.some((name) =>
    (servingComponent(document, name)?.runningImages.length ?? 0) === 0);
  if (missing || images.some(failed)) return 'mismatch';
  if (images.some((image) => image.signature !== 'verified')) {
    return 'unverified';
  }
  const rolling = document.components.some(({ runningImages }) =>
    runningImages.length > 1);
  if (rolling) {
    return 'rolling_out';
  }
  return 'verified';
}

/** The release rule of document.md, "Summary and release". */
export function recomputeRelease(
  document: DeploymentDocument,
): Release | null {
  if (recomputeSummary(document) !== 'verified') return null;
  const images = servingComponents.map((name) =>
    servingComponent(document, name)!.runningImages[0]!);
  const [first] = images;
  if (images.some((image) =>
    image.version !== first!.version || image.commit !== first!.commit)) {
    return null;
  }
  const deployedAt = images
    .map(({ runningSince }) => runningSince)
    .reduce((latest, since) =>
      Date.parse(since) > Date.parse(latest) ? since : latest);
  return { version: first!.version!, commit: first!.commit!, deployedAt };
}

function sameRelease(left: Release | null, right: Release | null): boolean {
  if (left === null || right === null) return left === right;
  return left.version === right.version && left.commit === right.commit
    && left.deployedAt === right.deployedAt;
}

/**
 * The server's clock at the moment of the response, from `Date` plus `Age`,
 * so a wrong visitor clock cannot make old data look current. Null when the
 * response carries no usable `Date`.
 */
export function responseTime(
  date: string | null,
  age: string | null,
): number | null {
  if (date === null) return null;
  const at = Date.parse(date);
  if (Number.isNaN(at)) return null;
  const seconds = age !== null && /^\d{1,9}$/u.test(age.trim())
    ? Number(age.trim())
    : 0;
  return at + seconds * 1000;
}

/** The staleness rule; with no trusted clock the document counts as stale. */
export function isStale(
  document: DeploymentDocument,
  now: number | null,
): boolean {
  if (now === null) return true;
  return now > Date.parse(document.staleAfter)
    || now - Date.parse(document.observedAt) > maxDocumentAgeMs;
}

export type MismatchReason = 'not_found' | 'invalid' | 'missing' | 'summary';

export interface Failure {
  readonly component: ComponentName;
  readonly reason: Exclude<MismatchReason, 'summary'>;
}

/** Each failing component in document order, with its first cause. */
export function failures(document: DeploymentDocument): Failure[] {
  const found: Failure[] = [];
  for (const name of componentNames) {
    const entry = servingComponent(document, name);
    const serving = servingComponents.includes(name);
    if (entry === undefined || entry.runningImages.length === 0) {
      if (serving) found.push({ component: name, reason: 'missing' });
      continue;
    }
    const bad = entry.runningImages.find(failed);
    if (bad !== undefined) {
      found.push({
        component: name,
        reason: bad.signature === 'invalid' ? 'invalid' : 'not_found',
      });
    }
  }
  return found;
}

/**
 * The version the chain shows: the release when there is one, otherwise the
 * newest verified version among the serving components (visual.md, chain).
 */
export interface ChainRelease {
  readonly version: string;
  readonly commit: string;
  readonly image: RunningImage;
}

function compareVersions(left: string, right: string): number {
  const parts = (value: string) => value.slice(1).split('.').map(Number);
  const [a, b] = [parts(left), parts(right)];
  for (let index = 0; index < 3; index += 1) {
    if (a[index] !== b[index]) return a[index]! - b[index]!;
  }
  return 0;
}

export function chainRelease(
  document: DeploymentDocument,
): ChainRelease | null {
  let best: RunningImage | null = null;
  for (const name of servingComponents) {
    for (const image of servingComponent(document, name)?.runningImages ?? []) {
      if (image.signature !== 'verified' || image.version === null) continue;
      if (document.release !== null
        && image.version !== document.release.version) continue;
      const order = best === null
        ? 1
        : compareVersions(image.version, best.version!)
          || Date.parse(image.runningSince) - Date.parse(best.runningSince);
      if (order > 0) best = image;
    }
  }
  if (best === null || best.version === null || best.commit === null) {
    return null;
  }
  return { version: best.version, commit: best.commit, image: best };
}

export type PageState
  = | { readonly kind: 'loading' }
    | { readonly kind: 'unavailable' }
    | { readonly kind: 'outdated' }
    | { readonly kind: 'stale'; readonly document: DeploymentDocument }
    | {
      readonly kind: 'mismatch';
      readonly document: DeploymentDocument;
      readonly reason: MismatchReason;
      /** Null for a summary disagreement. */
      readonly component: ComponentName | null;
      readonly more: number;
      /** The failure the chain shows; null for a summary disagreement. */
      readonly cause: Failure | null;
    }
    | {
      readonly kind: 'unverified';
      readonly document: DeploymentDocument;
      readonly component: ComponentName;
      readonly more: number;
    }
    | {
      readonly kind: 'rolling_out';
      readonly document: DeploymentDocument;
      readonly component: ComponentName;
      readonly from: string;
      readonly to: string;
    }
    | {
      readonly kind: 'verified';
      readonly document: DeploymentDocument;
      /** Null when the components run signed builds of different versions. */
      readonly release: Release | null;
    };

export type PageStateKind = PageState['kind'];

/** What the last fetch produced; `failed` covers network, status, and JSON. */
export type FetchOutcome
  = | { readonly kind: 'pending' }
    | { readonly kind: 'failed' }
    | { readonly kind: 'decoded'; readonly result: DecodeResult };

/** The state order of page.md: the first match wins. */
export function pageState(
  outcome: FetchOutcome,
  now: number | null,
): PageState {
  if (outcome.kind === 'pending') return { kind: 'loading' };
  if (outcome.kind === 'failed') return { kind: 'unavailable' };
  const { result } = outcome;
  if (result.kind === 'invalid') return { kind: 'unavailable' };
  if (result.kind === 'outdated') return { kind: 'outdated' };
  const { document } = result;
  if (isStale(document, now)) return { kind: 'stale', document };

  const summary = recomputeSummary(document);
  if (summary === 'mismatch') {
    const found = failures(document);
    const first = found[0]!;
    // A bad signature fails the Image step even when a missing component
    // comes first in document order; the chain shows the earliest break.
    const cause = found.find(({ reason }) => reason !== 'missing') ?? first;
    return {
      kind: 'mismatch',
      document,
      reason: first.reason,
      component: first.component,
      more: found.length - 1,
      cause,
    };
  }
  if (
    summary !== document.summary
    || !sameRelease(recomputeRelease(document), document.release)
  ) {
    return {
      kind: 'mismatch',
      document,
      reason: 'summary',
      component: null,
      more: 0,
      cause: null,
    };
  }
  if (summary === 'unverified') {
    const names = document.components
      .filter(({ runningImages }) =>
        runningImages.some(({ signature }) => signature !== 'verified'))
      .map(({ name }) => name);
    return {
      kind: 'unverified', document, component: names[0]!,
      more: names.length - 1,
    };
  }
  if (summary === 'rolling_out') {
    const entry = document.components.find(({ runningImages }) =>
      runningImages.length > 1)!;
    const images = entry.runningImages;
    return {
      kind: 'rolling_out',
      document,
      component: entry.name,
      from: images[0]!.version!,
      to: images[images.length - 1]!.version!,
    };
  }
  return { kind: 'verified', document, release: document.release };
}
