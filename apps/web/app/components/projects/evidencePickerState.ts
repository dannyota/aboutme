import {
  computed, getCurrentScope, onMounted, onScopeDispose, ref, watch,
  type ComputedRef, type Ref,
} from 'vue';

import type {
  GitHubEvidenceApi,
  GitHubEvidenceFailure,
} from '../../composables/githubEvidenceApi';
import type {
  CandidateQuery,
  EvidenceCandidate,
  EvidenceFailure,
  ProjectEvidenceApi,
} from '../../composables/projectEvidenceApi';
import { sendWithCsrf, type ProjectAuth } from '../../composables/projectsApi';
import { validateAuthorizeUrl } from '../../composables/providerAuthorization';
import type { EvidenceKind, Project } from './projectDocument';
import type {
  EvidenceNotice,
  ProjectEvidenceController,
} from './projectEvidenceState';

export type PickerTab = EvidenceKind | 'link';

export const PICKER_TABS: readonly PickerTab[] = [
  'repository', 'pullRequest', 'release', 'link',
];

export type PickerOutcome = 'connected' | 'failed' | 'different-account';

export interface ChosenRepository {
  readonly id: number;
  readonly fullName: string;
}

export interface Listing {
  readonly status: 'idle' | 'loading' | 'ready' | 'failed';
  readonly items: readonly EvidenceCandidate[];
  readonly nextPage: number | null;
  readonly incomplete: boolean;
  readonly failure: EvidenceNotice | null;
  readonly failedPage: number;
}

export interface EvidencePickerDeps {
  readonly evidence: Pick<
    ProjectEvidenceController,
    | 'attach'
    | 'items'
    | 'unsaved'
    | 'stale'
    | 'locked'
    | 'atLimit'
    | 'pending'
  >;
  readonly project: () => Project | null;
  readonly api: Pick<ProjectEvidenceApi, 'candidates'>;
  readonly github: Pick<GitHubEvidenceApi, 'start'>;
  readonly auth: ProjectAuth;
  readonly now?: () => number;
  readonly navigate?: (url: string) => unknown;
  readonly schedule?: (run: () => void, ms: number) => () => void;
  readonly onProjectGone?: () => void;
}

export interface ShowOptions {
  readonly tab?: PickerTab;
  readonly outcome?: PickerOutcome | null;
  readonly sessionEnded?: boolean;
  readonly opener?: HTMLElement | null;
}

export interface EvidencePicker {
  readonly open: Ref<boolean>;
  readonly tab: Ref<PickerTab>;
  readonly outcome: Ref<PickerOutcome | null>;
  readonly sessionEnded: Ref<boolean>;
  readonly repository: Ref<ChosenRepository | null>;
  readonly attaching: Ref<string | null>;
  readonly notice: Ref<EvidenceNotice | null>;
  readonly link: Ref<string>;
  readonly linkError: Ref<EvidenceNotice | null>;
  readonly starting: Ref<boolean>;
  readonly startFailure: Ref<GitHubEvidenceFailure | null>;
  readonly atLimit: ComputedRef<boolean>;
  readonly locked: ComputedRef<boolean>;
  readonly unsaved: ComputedRef<boolean>;
  readonly busy: ComputedRef<boolean>;
  readonly blocked: ComputedRef<boolean>;
  readonly firstAttach: ComputedRef<boolean>;
  opener(): HTMLElement | null;
  listing(tab: EvidenceKind): Listing;
  isAttached(candidate: EvidenceCandidate): boolean;
  show(options?: ShowOptions): void;
  close(): void;
  select(tab: PickerTab): void;
  more(tab: EvidenceKind): Promise<void>;
  retry(tab: EvidenceKind): Promise<void>;
  choose(candidate: EvidenceCandidate): void;
  changeRepository(): void;
  setLink(value: string): void;
  attachCandidate(candidate: EvidenceCandidate): Promise<boolean>;
  attachLink(): Promise<void>;
  continueWithGitHub(): Promise<void>;
}

type CandidateResult = Awaited<
  ReturnType<ProjectEvidenceApi['candidates']>
> | { readonly kind: 'failed'; readonly failure: 'session-lost' };

const SIGNED_OUT = {
  kind: 'failed', failure: 'session-lost', retryAfter: null,
} as const;

const IDLE: Listing = {
  status: 'idle',
  items: [],
  nextPage: null,
  incomplete: false,
  failure: null,
  failedPage: 0,
};

const START_RESET_MS = 5000;

function scheduleTimeout(run: () => void, ms: number): () => void {
  const handle = setTimeout(run, ms);
  return () => clearTimeout(handle);
}

const LINK_ERRORS = new Set<EvidenceNotice['kind']>([
  'unsupported-link', 'not-confirmed', 'duplicate',
]);

export function pastedLink(value: string): string {
  const trimmed = value.trim();
  return trimmed.startsWith('github.com/') ? `https://${trimmed}` : trimmed;
}

function linkPath(link: string): string {
  const trimmed = link.trim();
  try {
    return new URL(trimmed).pathname;
  } catch {
    const slash = trimmed.indexOf('/');
    return slash === -1 ? '' : trimmed.slice(slash).replace(/[?#].*$/u, '');
  }
}

export function linkKind(link: string): EvidenceKind {
  const segments = linkPath(link).split('/').filter((part) => part !== '');
  if (segments.length === 4 && segments[2] === 'pull') return 'pullRequest';
  if (
    segments.length === 5
    && segments[2] === 'releases'
    && segments[3] === 'tag'
  ) return 'release';
  return 'repository';
}

export function focusAfterPicker(opener: HTMLElement | null): void {
  const usable = (element: HTMLElement | null): element is HTMLElement =>
    element !== null
    && element.isConnected
    && !(element instanceof HTMLButtonElement && element.disabled);
  const add = document.getElementById('project-evidence-add');
  const target = [opener, add].find(usable)
    ?? document.getElementById('project-evidence-title');
  target?.focus();
}

function listingFailure(
  failure: EvidenceFailure,
  retryAfter: number | null,
  now: number,
): EvidenceNotice {
  switch (failure) {
    case 'rate-limited':
      return {
        kind: 'rate-limited',
        until: retryAfter === null ? null : now + retryAfter * 1000,
      };
    case 'upstream':
    case 'evidence-unavailable':
    case 'csrf-rejected':
    case 'session-lost':
      return { kind: failure };
    case 'busy':
    case 'unavailable':
      return { kind: 'try-again' };
    default:
      return { kind: 'failed' };
  }
}

export function useEvidencePicker(deps: EvidencePickerDeps): EvidencePicker {
  const now = deps.now ?? (() => Date.now());
  const navigate = deps.navigate
    ?? ((url: string) => navigateTo(url, { external: true }));
  const schedule = deps.schedule ?? scheduleTimeout;
  const open = ref(false);
  const tab = ref<PickerTab>('repository');
  const outcome = ref<PickerOutcome | null>(null);
  const sessionEnded = ref(false);
  const repository = ref<ChosenRepository | null>(null);
  const attaching = ref<string | null>(null);
  const attached = ref<readonly string[]>([]);
  const notice = ref<EvidenceNotice | null>(null);
  const link = ref('');
  const linkError = ref<EvidenceNotice | null>(null);
  const starting = ref(false);
  const startFailure = ref<GitHubEvidenceFailure | null>(null);
  const limitHit = ref(false);
  const listings = ref<Readonly<Record<string, Listing>>>({});
  const generations = new Map<string, number>();
  const inflight = new Map<string, Promise<unknown>>();
  let opener: HTMLElement | null = null;
  let cancelStartReset: (() => void) | null = null;
  let disposed = false;
  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      disposed = true;
      cancelStartReset?.();
      cancelStartReset = null;
    });
  }

  const atLimit = computed(() => deps.evidence.atLimit.value || limitHit.value);
  const locked = computed(() => deps.evidence.locked.value);
  const unsaved = computed(() => deps.evidence.unsaved.value);
  const busy = computed(() => deps.evidence.pending.value !== null);
  const blocked = computed(() =>
    locked.value || busy.value || attaching.value !== null);
  const firstAttach = computed(() => deps.evidence.items.value.length === 0);

  function keyOf(kind: EvidenceKind): string | null {
    if (kind !== 'release') return kind;
    const chosen = repository.value;
    return chosen === null ? null : `release:${chosen.id}`;
  }

  function listing(kind: EvidenceKind): Listing {
    const key = keyOf(kind);
    return key === null ? IDLE : listings.value[key] ?? IDLE;
  }

  function put(key: string, next: Listing): void {
    listings.value = { ...listings.value, [key]: next };
  }

  function drop(keys: readonly string[]): void {
    for (const key of keys) {
      generations.set(key, (generations.get(key) ?? 0) + 1);
    }
    listings.value = Object.fromEntries(Object.entries(listings.value)
      .filter(([key]) => !keys.includes(key)));
  }

  async function request(
    key: string,
    send: () => Promise<CandidateResult>,
  ): Promise<CandidateResult> {
    const before = inflight.get(key);
    if (before !== undefined) await before;
    const sent = send();
    inflight.set(key, sent);
    try {
      return await sent;
    } finally {
      if (inflight.get(key) === sent) inflight.delete(key);
    }
  }

  async function load(kind: EvidenceKind, page: number): Promise<void> {
    const key = keyOf(kind);
    const chosen = repository.value;
    const project = deps.project();
    const ownerId = deps.auth.user.value?.id;
    if (key === null || project === null) return;
    const current = listings.value[key] ?? IDLE;
    if (current.status === 'loading') return;
    const generation = generations.get(key) ?? 0;
    const stillWanted = () =>
      !disposed && (generations.get(key) ?? 0) === generation;
    put(key, { ...current, status: 'loading', failure: null });
    const query: CandidateQuery = kind === 'release' && chosen !== null
      ? { kind, page, repository: chosen.id }
      : { kind, page };
    const send = async (): Promise<CandidateResult> => {
      if (ownerId === undefined || !stillWanted()) return SIGNED_OUT;
      return sendWithCsrf(deps.auth, ownerId, (csrfToken) =>
        deps.api.candidates(project.id, query, csrfToken));
    };
    const result = await request(key, send);
    if (!stillWanted()) return;
    if (result.kind === 'failed' && result.failure === 'project-not-found') {
      put(key, current);
      open.value = false;
      deps.onProjectGone?.();
      return;
    }
    if (result.kind === 'ok') {
      put(key, {
        status: 'ready',
        items: page === 1
          ? result.value.items
          : [...current.items, ...result.value.items],
        nextPage: result.value.nextPage,
        incomplete: (page > 1 && current.incomplete)
          || result.value.incomplete,
        failure: null,
        failedPage: 0,
      });
      return;
    }
    if (result.kind === 'failed' && result.failure === 'session-required') {
      sessionEnded.value = true;
      put(key, current);
      return;
    }
    const retryAfter = 'retryAfter' in result ? result.retryAfter : null;
    put(key, {
      ...current,
      status: 'failed',
      failure: result.kind === 'failed'
        ? listingFailure(result.failure, retryAfter, now())
        : { kind: 'failed' },
      failedPage: page,
    });
  }

  function ensure(next: PickerTab): void {
    if (next === 'link' || sessionEnded.value) return;
    const kind = next === 'release' && repository.value === null
      ? 'repository'
      : next;
    if (listing(kind).status === 'idle') void load(kind, 1);
  }

  function select(next: PickerTab): void {
    tab.value = next;
    ensure(next);
  }

  watch(() => deps.evidence.items.value.map((item) => item.id),
    (next, previous) => {
      if (!previous.some((id) => !next.includes(id))) return;
      drop([...new Set([
        ...Object.keys(listings.value), ...inflight.keys(),
      ])]);
      attached.value = [];
      if (open.value) ensure(tab.value);
    });

  watch(() => deps.evidence.stale.value, (stale) => {
    if (stale) open.value = false;
  });

  function chooserKind(kind: EvidenceKind): EvidenceKind {
    return kind === 'release' && repository.value === null
      ? 'repository'
      : kind;
  }

  async function attachCandidate(
    candidate: EvidenceCandidate,
  ): Promise<boolean> {
    if (blocked.value || atLimit.value) return false;
    attaching.value = candidate.candidateId;
    notice.value = null;
    const result = await deps.evidence.attach({
      kind: candidate.kind, candidateId: candidate.candidateId,
    });
    attaching.value = null;
    switch (result.kind) {
      case 'attached':
        attached.value = [...attached.value, candidate.candidateId];
        return true;
      case 'session-ended':
        sessionEnded.value = true;
        return false;
      case 'closed':
        open.value = false;
        return false;
      case 'failed':
        if (result.notice.kind === 'limit') limitHit.value = true;
        if (result.notice.kind === 'duplicate') {
          attached.value = [...attached.value, candidate.candidateId];
        }
        notice.value = result.notice;
        return false;
      default:
        return false;
    }
  }

  async function attachLink(): Promise<void> {
    const value = pastedLink(link.value);
    if (value === '' || blocked.value || atLimit.value) return;
    attaching.value = 'link';
    notice.value = null;
    linkError.value = null;
    const result = await deps.evidence.attach({
      kind: linkKind(value), link: value,
    });
    attaching.value = null;
    if (result.kind === 'attached') {
      link.value = '';
    } else if (result.kind === 'session-ended') {
      sessionEnded.value = true;
    } else if (result.kind === 'closed') {
      open.value = false;
    } else if (result.kind === 'failed') {
      if (result.notice.kind === 'limit') limitHit.value = true;
      if (LINK_ERRORS.has(result.notice.kind)) {
        linkError.value = result.notice;
      } else {
        notice.value = result.notice;
      }
    }
  }

  async function continueWithGitHub(): Promise<void> {
    const project = deps.project();
    const ownerId = deps.auth.user.value?.id;
    if (
      locked.value
      || busy.value
      || starting.value
      || project === null
    ) return;
    starting.value = true;
    startFailure.value = null;
    const result = ownerId === undefined
      ? { kind: 'failed', failure: 'session-lost' } as const
      : await sendWithCsrf(deps.auth, ownerId, (csrfToken) =>
          deps.github.start(csrfToken, `/app/projects/${project.id}`));
    if (disposed) return;
    const url = result.kind === 'ok'
      ? validateAuthorizeUrl('github', result.value)
      : null;
    if (url === null) {
      starting.value = false;
      startFailure.value = result.kind === 'ok' ? 'failed' : result.failure;
      return;
    }
    await navigate(url);
    if (disposed) return;
    cancelStartReset?.();
    cancelStartReset = schedule(() => {
      cancelStartReset = null;
      starting.value = false;
    }, START_RESET_MS);
  }

  return {
    open,
    tab,
    outcome,
    sessionEnded,
    repository,
    attaching,
    notice,
    link,
    linkError,
    starting,
    startFailure,
    atLimit,
    locked,
    unsaved,
    busy,
    blocked,
    firstAttach,
    opener: () => opener,
    listing: (kind) => listing(chooserKind(kind)),
    isAttached: (candidate) => candidate.attached
      || attached.value.includes(candidate.candidateId),
    show(options = {}) {
      opener = options.opener ?? null;
      if (options.tab !== undefined) tab.value = options.tab;
      if (options.sessionEnded === true) sessionEnded.value = true;
      outcome.value = options.outcome ?? null;
      notice.value = null;
      linkError.value = null;
      startFailure.value = null;
      limitHit.value = false;
      open.value = true;
      ensure(tab.value);
    },
    close() {
      open.value = false;
      outcome.value = null;
    },
    select,
    more: async (kind) => {
      const next = listing(chooserKind(kind)).nextPage;
      if (next !== null) await load(chooserKind(kind), next);
    },
    retry: async (kind) => {
      const current = listing(chooserKind(kind));
      await load(chooserKind(kind), current.failedPage || 1);
    },
    choose(candidate) {
      const id = candidate.repository.id;
      if (id === null) return;
      repository.value = { id, fullName: candidate.repository.fullName };
      ensure('release');
    },
    changeRepository() {
      repository.value = null;
      drop(Object.keys(listings.value)
        .filter((key) => key.startsWith('release:')));
      ensure('release');
    },
    setLink(value) {
      link.value = value;
    },
    attachCandidate,
    attachLink,
    continueWithGitHub,
  };
}

function outcomeOf(value: unknown): PickerOutcome | null {
  if (value === 'connected' || value === 'failed') return value;
  return value === 'different_account' ? 'different-account' : null;
}

export function useEvidenceReturn(
  picker: Pick<EvidencePicker, 'show'>,
  ready: Readonly<Ref<boolean>>,
): void {
  const route = useRoute();
  const router = useRouter();
  onMounted(() => {
    if (!('evidence' in route.query)) return;
    let outcome = outcomeOf(route.query.evidence);
    const query = Object.fromEntries(
      Object.entries(route.query).filter(([key]) => key !== 'evidence'),
    );
    void router.replace({ path: route.path, query, hash: route.hash });
    watch(ready, (isReady) => {
      if (!isReady || outcome === null) return;
      picker.show({ tab: 'repository', outcome });
      outcome = null;
    }, { immediate: true });
  });
}
