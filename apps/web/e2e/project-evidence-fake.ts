import type { Page, Route } from '@playwright/test';

export interface EvidenceItem {
  id: string;
  kind: 'repository' | 'pullRequest' | 'release';
  url: string;
  repositoryName: string;
  title: string;
  itemDate: string;
  checkedAt: string;
  confirmed: boolean;
  flagReason: 'rule_failed' | 'gone' | null;
}

export interface Candidate {
  readonly candidateId: string;
  readonly kind: EvidenceItem['kind'];
  readonly title: string;
  readonly repository: {
    readonly id: number | null;
    readonly fullName: string;
  };
  readonly date: string;
  readonly attached: boolean;
  readonly url: string;
}

export interface CandidatePage {
  readonly items: readonly Candidate[];
  readonly nextPage: number | null;
}

export interface EvidenceProject {
  revision: string;
  evidence?: EvidenceItem[];
}

export interface EvidenceOptions {
  readonly pages?: Readonly<Partial<Record<
    EvidenceItem['kind'], readonly CandidatePage[]
  >>>;
  readonly sessionEnded?: boolean;
  readonly recheck?: (item: EvidenceItem) => EvidenceItem;
}

export interface EvidenceWrite {
  readonly method: string;
  readonly path: string;
  readonly ifMatch: string | null;
  readonly key: string | null;
  readonly body: unknown;
}

const HEADERS = { 'Cache-Control': 'no-store, no-transform' };
const CHECKED = '2026-11-18T09:00:00Z';
const LINK = new RegExp(
  '^https://github\\.com/([^/\\s?#]+)/([^/\\s?#]+)'
  + '(?:/pull/([0-9]+)|/releases/tag/([^/\\s?#]+))?/?$', 'u',
);

function problem(status: number, code: string, details?: unknown) {
  return {
    status,
    headers: HEADERS,
    json: {
      error: { code, message: 'fixed', ...(details ? { details } : {}) },
    },
  };
}

function fromLink(link: string, count: number): EvidenceItem | null {
  const match = LINK.exec(link.trim());
  if (match === null) return null;
  const [, owner, repo, pull, tag] = match;
  const repositoryName = `${owner}/${repo}`;
  const kind = pull !== undefined
    ? 'pullRequest'
    : tag === undefined ? 'repository' : 'release';
  return {
    id: `0190f000-0000-7000-8000-0000000e${String(count).padStart(4, '0')}`,
    kind,
    url: link.trim().replace(/\/$/u, ''),
    repositoryName,
    title: pull === undefined
      ? tag ?? repositoryName
      : `${repositoryName}#${pull}`,
    itemDate: '2026-01-02T03:04:05Z',
    checkedAt: CHECKED,
    confirmed: true,
    flagReason: null,
  };
}

export async function fakeEvidence(
  page: Page,
  projects: Map<string, EvidenceProject>,
  options: EvidenceOptions = {},
) {
  const writes: EvidenceWrite[] = [];
  const reads: string[] = [];
  const starts: unknown[] = [];
  let sessionEnded = options.sessionEnded ?? false;
  let created = 0;
  const candidates = new Map(Object.values(options.pages ?? {})
    .flatMap((pages) => pages.flatMap((entry) => entry.items))
    .map((item) => [item.candidateId, item]));

  async function written(route: Route, project: EvidenceProject) {
    project.revision = String(Number(project.revision) + 1);
    await route.fulfill({
      headers: HEADERS,
      json: {
        data: {
          evidence: project.evidence,
          projectRevision: project.revision,
          resumeRevisions: [],
        },
      },
    });
  }

  await page.route(/\/api\/v1\/projects\/[^/]+\/evidence(?:[/?].*)?$/u,
    async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const parts = url.pathname.split('/');
      const project = projects.get(parts[4] ?? '');
      const method = request.method();
      if (request.headers()['x-csrf-token'] !== 'csp-test-token') {
        await route.fulfill(problem(403, 'csrf_rejected'));
        return;
      }
      if (project?.evidence === undefined) {
        await route.fulfill(problem(404, 'project_not_found'));
        return;
      }
      if (parts[6] === 'candidates') {
        reads.push(url.search);
        if (sessionEnded) {
          await route.fulfill(problem(409, 'evidence_session_required'));
          return;
        }
        const kind = url.searchParams.get('kind') as EvidenceItem['kind'];
        const index = Number(url.searchParams.get('page')) - 1;
        const entry = options.pages?.[kind]?.[index]
          ?? { items: [], nextPage: null };
        const attached = new Set(project.evidence.map((item) => item.url));
        await route.fulfill({
          headers: HEADERS,
          json: {
            data: {
              items: entry.items.map(({ url: link, ...item }) => ({
                ...item, attached: item.attached || attached.has(link),
              })),
              nextPage: entry.nextPage,
              incomplete: false,
            },
          },
        });
        return;
      }
      writes.push({
        method, path: url.pathname,
        ifMatch: request.headers()['if-match'] ?? null,
        key: request.headers()['idempotency-key'] ?? null,
        body: request.postData() ? request.postDataJSON() : null,
      });
      if (request.headers()['if-match'] !== `"r${project.revision}"`) {
        await route.fulfill(problem(412, 'revision_mismatch', {
          revision: project.revision, project,
        }));
        return;
      }
      const items = project.evidence;
      if (method === 'POST' && parts.length === 6) {
        const body = request.postDataJSON() as {
          kind: EvidenceItem['kind'];
          link?: string;
          candidateId?: string;
        };
        created += 1;
        const chosen = body.candidateId === undefined
          ? undefined
          : candidates.get(body.candidateId);
        const item = fromLink(chosen?.url ?? body.link ?? '', created);
        if (item === null || item.kind !== body.kind) {
          await route.fulfill(problem(422, 'evidence_unsupported_link'));
          return;
        }
        if (chosen !== undefined) {
          item.title = chosen.title;
          item.itemDate = chosen.date;
        }
        project.evidence = [...items, item];
        await written(route, project);
        return;
      }
      if (method === 'PUT') {
        const { order } = request.postDataJSON() as { order: string[] };
        project.evidence = order.map((id) =>
          items.find((item) => item.id === id)!);
        await written(route, project);
        return;
      }
      const index = items.findIndex((item) => item.id === parts[6]);
      if (index === -1) {
        await route.fulfill(problem(404, 'evidence_not_found'));
        return;
      }
      if (method === 'DELETE') {
        project.evidence = items.filter((_, at) => at !== index);
      } else {
        const next = options.recheck ?? ((item: EvidenceItem) => item);
        project.evidence = items.map((item, at) =>
          at === index ? { ...next(item), checkedAt: CHECKED } : item);
      }
      await written(route, project);
    });
  await page.route('**/api/v1/github-evidence/start', async (route) => {
    const request = route.request();
    starts.push(request.postData() ? request.postDataJSON() : null);
    await route.fulfill({
      headers: HEADERS,
      json: {
        data: {
          authorizeUrl: 'https://github.com/login/oauth/authorize'
            + '?client_id=evidence&state=s&code_challenge=c'
            + '&code_challenge_method=S256',
        },
      },
    });
  });
  return {
    writes,
    reads,
    starts,
    resumeSession: () => {
      sessionEnded = false;
    },
  };
}
