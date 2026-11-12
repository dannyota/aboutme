import {
  computed, getCurrentScope, onScopeDispose, ref, watch,
  type ComputedRef, type Ref,
} from 'vue';

import type { Locale } from '../i18n/locale';
import {
  createProjectLinker,
  rememberUnlinked,
  type ProjectLinker,
} from '../components/projects/projectLinks';
import {
  canonicalJSON,
  draftFromProject,
  emptyText,
  isLanguageKey,
  LANGUAGE_LIMIT,
  PROJECT_LIMIT,
  requestFromDraft,
  sameDocument,
  validateDraft,
  type Project,
  type ProjectDraft,
  type ProjectIssue,
} from '../components/projects/projectDocument';
import { choosesTwoPublished } from '../editor/projectLinks';
import type { ResumeSummary } from '../editor/resumeApi';
import {
  createProjectsApi,
  sendWithCsrf,
  type ProjectAuth,
  type ProjectsApi,
} from './projectsApi';
import { useAuth, type AuthState } from './useAuth';

export interface ProjectsDeps {
  readonly api?: ProjectsApi;
  readonly auth?: ProjectAuth;
  readonly uuid?: () => string;
  readonly navigate?: (path: string) => unknown;
  readonly linker?: ProjectLinker;
}

interface ResolvedDeps {
  readonly api: ProjectsApi;
  readonly auth: ProjectAuth;
  readonly uuid: () => string;
  readonly navigate: (path: string) => unknown;
}

function resolve(deps: ProjectsDeps): ResolvedDeps {
  return {
    api: deps.api ?? createProjectsApi(),
    auth: deps.auth ?? useAuth(),
    uuid: deps.uuid ?? (() => crypto.randomUUID()),
    navigate: deps.navigate ?? ((path) => navigateTo(path)),
  };
}

/** A key per user intent, kept only while the same request may be retried. */
function intentKeys(uuid: () => string) {
  let pending: { fingerprint: string; key: string } | null = null;
  return {
    keyFor(request: unknown): string {
      const fingerprint = canonicalJSON(request);
      if (pending?.fingerprint !== fingerprint) {
        pending = { fingerprint, key: uuid() };
      }
      return pending.key;
    },
    settle(): void {
      pending = null;
    },
  };
}

/**
 * Follows the session like the resume list: a first anonymous visit is the
 * route guard's job; a session lost after a read sends the page to sign in.
 */
function followSession(
  authState: Readonly<Ref<AuthState>>,
  handlers: {
    readonly waiting: () => void;
    readonly signedIn: () => void;
    readonly failed: () => void;
    readonly lost: () => void;
  },
): void {
  let wasAuthenticated = false;
  watch(authState, (state) => {
    if (state === 'loading') {
      handlers.waiting();
    } else if (state === 'authenticated') {
      wasAuthenticated = true;
      handlers.signedIn();
    } else if (state === 'anonymous') {
      if (wasAuthenticated) handlers.lost();
    } else {
      handlers.failed();
    }
  }, { immediate: true });
}

function useDisposed(): Ref<boolean> {
  const disposed = ref(false);
  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      disposed.value = true;
    });
  }
  return disposed;
}

export type ProjectListView
  = | { readonly kind: 'waiting-auth' }
    | { readonly kind: 'loading' }
    | { readonly kind: 'ready'; readonly items: readonly Project[] }
    | { readonly kind: 'unavailable' };

export type CreateState
  = 'idle' | 'busy' | 'limit' | 'try-again' | 'invalid' | 'session-lost';

export interface ProjectListController {
  readonly view: Ref<ProjectListView>;
  /** Newest update first. */
  readonly items: ComputedRef<readonly Project[]>;
  readonly atLimit: ComputedRef<boolean>;
  readonly createState: Ref<CreateState>;
  /** The account's resumes a new project can be shown on. */
  readonly resumes: Ref<readonly ResumeSummary[]>;
  /** The privacy note a submit decided to show; cleared when it opens. */
  readonly privacyNote: Ref<boolean>;
  retry(): Promise<void>;
  loadResumes(): Promise<void>;
  /** Creates the project, links each chosen resume, then opens it. */
  create(name: string, resumeIds?: readonly string[]): Promise<void>;
  resetCreate(): void;
  adopt(project: Project): void;
  forget(id: string): void;
}

export function useProjectList(
  locale: Readonly<Ref<Locale>>,
  deps: ProjectsDeps = {},
): ProjectListController {
  const { api, auth, uuid, navigate } = resolve(deps);
  const linker = deps.linker ?? createProjectLinker({ auth });
  const view = ref<ProjectListView>({ kind: 'waiting-auth' });
  const createState = ref<CreateState>('idle');
  const resumes = ref<readonly ResumeSummary[]>([]);
  const privacyNote = ref(false);
  const keys = intentKeys(uuid);
  const disposed = useDisposed();

  const items = computed<readonly Project[]>(() =>
    view.value.kind === 'ready'
      ? [...view.value.items].sort((left, right) =>
          Date.parse(right.updatedAt) - Date.parse(left.updatedAt))
      : []);
  const atLimit = computed(() => items.value.length >= PROJECT_LIMIT);

  async function load(): Promise<void> {
    view.value = { kind: 'loading' };
    const result = await api.list();
    if (disposed.value) return;
    if (result.kind === 'ok') {
      view.value = { kind: 'ready', items: result.value };
    } else if (result.kind === 'failed' && result.failure === 'session-lost') {
      await navigate('/login');
    } else {
      view.value = { kind: 'unavailable' };
    }
  }

  followSession(auth.authState, {
    waiting: () => {
      view.value = { kind: 'waiting-auth' };
    },
    signedIn: () => void load(),
    failed: () => {
      view.value = { kind: 'unavailable' };
    },
    lost: () => void navigate('/login'),
  });

  function setItems(next: readonly Project[]): void {
    if (view.value.kind === 'ready') {
      view.value = { kind: 'ready', items: next };
    }
  }

  async function loadResumes(): Promise<void> {
    const result = await linker.resumes();
    if (!disposed.value) {
      resumes.value = result.kind === 'ready' ? result.items : [];
    }
  }

  /** Links each resume; the editor names the ones that did not take it. */
  async function linkChosen(
    ownerId: string,
    projectId: string,
    resumeIds: readonly string[],
  ): Promise<void> {
    const missed: string[] = [];
    for (const resumeId of resumeIds) {
      const result = await linker.link(ownerId, resumeId, projectId);
      if (result.kind === 'failed' && result.failure !== 'already-linked') {
        missed.push(resumes.value.find((item) => item.id === resumeId)?.title
          ?? '');
      }
    }
    rememberUnlinked(projectId, missed);
  }

  /**
   * The privacy note, decided at submit for the resumes checked then, from
   * the account's resumes read again (docs/design/projects.md "Privacy").
   * When it applies and the dialog did not show it, it appears and nothing
   * is created until the owner submits again. A failed read shows it for
   * two or more checked resumes, since it cannot rule them out.
   */
  async function privacyNoteAppeared(
    resumeIds: readonly string[],
  ): Promise<boolean> {
    const shown = privacyNote.value
      || choosesTwoPublished(resumes.value, resumeIds);
    createState.value = 'busy';
    const result = await linker.resumes();
    createState.value = 'idle';
    if (disposed.value) return true;
    if (result.kind === 'ready') resumes.value = result.items;
    const applies = result.kind === 'ready'
      ? choosesTwoPublished(result.items, resumeIds)
      : resumeIds.length >= 2;
    if (!applies || shown) return false;
    privacyNote.value = true;
    return true;
  }

  async function create(
    name: string,
    resumeIds: readonly string[] = [],
  ): Promise<void> {
    const trimmed = name.trim();
    if (createState.value === 'busy') return;
    if (trimmed === '') {
      createState.value = 'invalid';
      return;
    }
    if (atLimit.value) {
      createState.value = 'limit';
      return;
    }
    const ownerId = auth.user.value?.id;
    if (ownerId === undefined) {
      createState.value = 'session-lost';
      return;
    }
    if (resumeIds.length > 0 && await privacyNoteAppeared(resumeIds)) return;
    const language = locale.value;
    const body = {
      defaultLanguage: language,
      text: { [language]: { name: trimmed } },
    };
    const key = keys.keyFor({ ownerId, body });
    createState.value = 'busy';
    const result = await sendWithCsrf(auth, ownerId, (csrfToken) =>
      api.create(body, key, csrfToken));
    if (disposed.value) return;
    if (result.kind === 'ok') {
      keys.settle();
      setItems([...items.value, result.value]);
      await linkChosen(ownerId, result.value.id, resumeIds);
      createState.value = 'idle';
      if (disposed.value) return;
      await navigate(`/app/projects/${encodeURIComponent(result.value.id)}`);
      return;
    }
    const failure = result.kind === 'failed' ? result.failure : 'unavailable';
    if (failure === 'rate-limited' || failure === 'unavailable'
      || failure === 'csrf-rejected') {
      createState.value = 'try-again';
      return;
    }
    keys.settle();
    createState.value = failure === 'limit' || failure === 'session-lost'
      ? failure
      : 'invalid';
  }

  return {
    view,
    items,
    atLimit,
    createState,
    resumes,
    privacyNote,
    retry: load,
    loadResumes,
    create,
    resetCreate: () => {
      if (createState.value !== 'busy') createState.value = 'idle';
      privacyNote.value = false;
    },
    adopt: (project) => setItems(items.value.map((item) =>
      item.id === project.id ? project : item)),
    forget: (id) => setItems(items.value.filter((item) => item.id !== id)),
  };
}

export type ProjectEditorView
  = 'waiting-auth' | 'loading' | 'ready' | 'not-found' | 'unavailable';

export type ProjectEditorNotice
  = 'changed-elsewhere' | 'try-again' | 'invalid' | 'session-lost' | null;

export interface ProjectEditorController {
  readonly view: Ref<ProjectEditorView>;
  readonly loaded: Ref<Project | null>;
  readonly draft: Ref<ProjectDraft | null>;
  /** Advances when a reload replaces the typed text, to remount fields. */
  readonly generation: Ref<number>;
  readonly dirty: ComputedRef<boolean>;
  readonly saving: Ref<boolean>;
  readonly saveFailed: Ref<boolean>;
  readonly issues: Ref<readonly ProjectIssue[]>;
  readonly notice: Ref<ProjectEditorNotice>;
  readonly canAddLanguage: ComputedRef<boolean>;
  retry(): Promise<void>;
  save(extraIssues?: readonly ProjectIssue[]): Promise<void>;
  reload(): Promise<void>;
  adopt(project: Project): void;
  markStale(project: Project): void;
  addLanguage(key: string): boolean;
  makeDefault(key: string): void;
  removeLanguage(key: string): void;
}

export function useProjectEditor(
  id: string,
  deps: ProjectsDeps = {},
): ProjectEditorController {
  const { api, auth, uuid, navigate } = resolve(deps);
  const view = ref<ProjectEditorView>('waiting-auth');
  const loaded = ref<Project | null>(null);
  const draft = ref<ProjectDraft | null>(null);
  const generation = ref(0);
  const saving = ref(false);
  const saveFailed = ref(false);
  const issues = ref<readonly ProjectIssue[]>([]);
  const notice = ref<ProjectEditorNotice>(null);
  const keys = intentKeys(uuid);
  const disposed = useDisposed();
  let winner: Project | null = null;
  // Saves belong to the account that loaded the project.
  let ownerId: string | null = null;

  const dirty = computed(() =>
    loaded.value !== null
    && draft.value !== null
    && !sameDocument(draft.value, draftFromProject(loaded.value)));
  const canAddLanguage = computed(() =>
    draft.value !== null
    && Object.keys(draft.value.text).length < LANGUAGE_LIMIT);

  function take(project: Project): void {
    winner = null;
    loaded.value = project;
    draft.value = draftFromProject(project);
    generation.value += 1;
    issues.value = [];
    notice.value = null;
    saveFailed.value = false;
    keys.settle();
  }

  async function load(): Promise<void> {
    view.value = 'loading';
    ownerId = auth.user.value?.id ?? null;
    const result = await api.read(id);
    if (disposed.value) return;
    if (result.kind === 'ok') {
      take(result.value);
      view.value = 'ready';
    } else if (result.kind === 'failed' && result.failure === 'not-found') {
      view.value = 'not-found';
    } else if (result.kind === 'failed' && result.failure === 'session-lost') {
      await navigate('/login');
    } else {
      view.value = 'unavailable';
    }
  }

  followSession(auth.authState, {
    waiting: () => {
      if (loaded.value === null) view.value = 'waiting-auth';
    },
    signedIn: () => {
      if (loaded.value === null) void load();
    },
    failed: () => {
      if (loaded.value === null) view.value = 'unavailable';
    },
    // Typed text stays in memory; the next save reports the lost session.
    lost: () => {
      if (!dirty.value) void navigate('/login');
    },
  });

  function markStale(project: Project): void {
    winner = project;
    notice.value = 'changed-elsewhere';
    saveFailed.value = true;
  }

  async function save(
    extraIssues: readonly ProjectIssue[] = [],
  ): Promise<void> {
    const current = draft.value;
    const base = loaded.value;
    if (current === null || base === null || saving.value) return;
    const found = [...extraIssues, ...validateDraft(current)];
    issues.value = found;
    if (found.length > 0) return;
    if (ownerId === null) {
      notice.value = 'session-lost';
      return;
    }
    const body = requestFromDraft(current);
    const revision = base.revision;
    const key = keys.keyFor({ ownerId, revision, body });
    saving.value = true;
    notice.value = null;
    const result = await sendWithCsrf(auth, ownerId, (csrfToken) =>
      api.replace(id, revision, body, key, csrfToken));
    saving.value = false;
    if (disposed.value) return;
    if (result.kind === 'ok') {
      keys.settle();
      winner = null;
      saveFailed.value = false;
      loaded.value = result.value;
      // The server's sanitized text replaces the draft only when nothing was
      // typed while the save ran.
      const typed = draft.value === null
        ? null
        : canonicalJSON(requestFromDraft(draft.value));
      if (typed === canonicalJSON(body)) {
        draft.value = draftFromProject(result.value);
      }
      return;
    }
    saveFailed.value = true;
    if (result.kind === 'stale') {
      keys.settle();
      markStale(result.project);
      return;
    }
    const failure = result.failure;
    if (failure === 'not-found') {
      keys.settle();
      view.value = 'not-found';
    } else if (failure === 'invalid') {
      keys.settle();
      notice.value = 'invalid';
    } else if (failure === 'session-lost') {
      notice.value = 'session-lost';
    } else {
      notice.value = 'try-again';
    }
  }

  async function reload(): Promise<void> {
    if (saving.value) return;
    if (winner !== null) {
      take(winner);
      return;
    }
    const result = await api.read(id);
    if (disposed.value) return;
    if (result.kind === 'ok') take(result.value);
    else if (result.kind === 'failed' && result.failure === 'not-found') {
      view.value = 'not-found';
    } else notice.value = 'try-again';
  }

  return {
    view,
    loaded,
    draft,
    generation,
    dirty,
    saving,
    saveFailed,
    issues,
    notice,
    canAddLanguage,
    retry: load,
    save,
    reload,
    adopt(project) {
      loaded.value = project;
      if (draft.value !== null) draft.value.slug = project.slug;
      winner = null;
    },
    markStale,
    addLanguage(key) {
      const current = draft.value;
      if (
        current === null
        || !canAddLanguage.value
        || !isLanguageKey(key)
        || key in current.text
      ) return false;
      current.text[key] = emptyText();
      return true;
    },
    makeDefault(key) {
      if (draft.value !== null && key in draft.value.text) {
        draft.value.defaultLanguage = key;
      }
    },
    removeLanguage(key) {
      const current = draft.value;
      if (current === null || key === current.defaultLanguage) return;
      current.text = Object.fromEntries(Object.entries(current.text)
        .filter(([language]) => language !== key));
    },
  };
}
