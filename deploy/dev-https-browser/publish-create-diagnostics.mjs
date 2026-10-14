import { writeFile } from 'node:fs/promises';
import { observeImportNetwork } from './linkedin-import-diagnostics.mjs';

// Record creation stages, request outcomes, and control state at Enter to
// distinguish intermittent failures without retaining volatile browser output.
const STAGES = [
  'navigate-list', 'ready', 'open-dialog', 'blank-selected', 'title-filled',
  'submitted', 'response-created', 'response-cap-exceeded', 'response-other',
];
const word = (value, allowed, fallback) => allowed.includes(value) ? value : fallback;
const flag = (value) => typeof value === 'boolean' ? value : null;
const count = (value) => Number.isSafeInteger(value) && value >= 0 ? Math.min(value, 1000) : 0;
const status = (value) => Number.isInteger(value) && value >= 100 && value <= 599 ? value : 0;

function failureKind(error) {
  const message = typeof error?.message === 'string' ? error.message : '';
  if (/ERR_CERT/u.test(message)) return 'certificate';
  if (/ERR_CONNECTION_REFUSED/u.test(message)) return 'connection-refused';
  if (/ERR_ABORTED/u.test(message)) return 'aborted';
  if (error?.name === 'TimeoutError' || /Timeout|timed out/u.test(message)) return 'timeout';
  if (error?.name === 'SyntaxError') return 'json';
  return /expect\(/u.test(message) ? 'assertion' : 'other';
}

// Build the artifact from closed fields, including when browser state is unreadable.
export function createFailureEvidence(lastStage, error, network, state) {
  const stack = typeof error?.stack === 'string' ? error.stack : '';
  const line = /editor-fixtures\.ts:([0-9]{1,4}):/u.exec(stack)?.[1];
  return {
    schemaVersion: 1,
    lastStage: word(lastStage, STAGES, 'unknown'),
    helperLine: line === undefined ? 0 : Number(line),
    failure: failureKind(error),
    network: {
      createRequests: count(network?.createRequests),
      createResponses: count(network?.createResponses),
      createStatus: status(network?.createStatus),
      createFailed: flag(network?.createFailed),
      meStatus: status(network?.meStatus),
      listStatus: status(network?.listStatus),
    },
    page: {
      path: state === null ? 'unread' : word(state?.path, ['list', 'editor', 'login'], 'other'),
      readyState: word(state?.readyState, ['loading', 'interactive', 'complete'], 'unread'),
      lang: word(state?.lang, ['en', 'vi'], 'other'),
      hydrated: flag(state?.hydrated),
      createDisabled: flag(state?.createDisabled),
      submitDisabled: flag(state?.submitDisabled),
      dialogVisible: flag(state?.dialogVisible),
      input: {
        enterCount: count(state?.input?.enterCount),
        openClicks: count(state?.input?.openClicks),
        submitClicks: count(state?.input?.submitClicks),
        submits: count(state?.input?.submits),
        target: word(state?.input?.target, ['create', 'submit', 'dialog', 'outside'], 'unread'),
        hydrated: flag(state?.input?.hydrated),
        createDisabled: flag(state?.input?.createDisabled),
        submitDisabled: flag(state?.input?.submitDisabled),
      },
    },
  };
}

function installInputObserver() {
  const input = {
    enterCount: 0, openClicks: 0, submitClicks: 0, submits: 0,
    target: 'unread', hydrated: null, createDisabled: null, submitDisabled: null,
  };
  Reflect.set(window, '__publishCreateInput', input);
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Enter') return;
    input.enterCount++;
    const target = event.target;
    input.target = target?.closest?.('[data-testid="create-resume"]') ? 'create'
      : target?.closest?.('[role="dialog"] [type="submit"]') ? 'submit'
        : target?.closest?.('[role="dialog"]') ? 'dialog' : 'outside';
    input.hydrated = Boolean(document.getElementById('__nuxt')?.__vue_app__);
    input.createDisabled = document.querySelector('[data-testid="create-resume"]')?.disabled ?? null;
    input.submitDisabled = document.querySelector('[role="dialog"] [type="submit"]')?.disabled ?? null;
  }, true);
  document.addEventListener('click', (event) => {
    if (event.target?.closest?.('[data-testid="create-resume"]')) input.openClicks++;
    if (event.target?.closest?.('[role="dialog"] [type="submit"]')) input.submitClicks++;
  }, true);
  document.addEventListener('submit', (event) => {
    if (event.target?.closest?.('[role="dialog"]')) input.submits++;
  }, true);
}

function readPageState() {
  const dialog = document.querySelector('[role="dialog"]');
  const path = location.pathname === '/app/resumes' ? 'list'
    : location.pathname.startsWith('/app/resumes/') ? 'editor'
      : location.pathname === '/login' ? 'login' : 'other';
  return {
    path,
    readyState: document.readyState,
    lang: ['en', 'vi'].includes(document.documentElement.lang) ? document.documentElement.lang : 'other',
    hydrated: Boolean(document.getElementById('__nuxt')?.__vue_app__),
    createDisabled: document.querySelector('[data-testid="create-resume"]')?.disabled ?? null,
    submitDisabled: dialog?.querySelector('[type="submit"]')?.disabled ?? null,
    dialogVisible: dialog !== null && dialog.getClientRects().length > 0,
    input: Reflect.get(window, '__publishCreateInput') ?? null,
  };
}

export async function observePublishCreate(page) {
  const network = {
    createRequests: 0, createResponses: 0, createStatus: 0,
    createFailed: false, meStatus: 0, listStatus: 0,
  };
  let lastStage = 'navigate-list';
  const lifecycle = observeImportNetwork(page, () => lastStage);
  const route = (request) => {
    const url = new URL(request.url());
    if (url.origin !== 'https://localhost:20443') return 'other';
    if (url.pathname === '/api/v1/resumes' && request.method() === 'POST') return 'create';
    if (url.pathname === '/api/v1/resumes' && request.method() === 'GET') return 'list';
    if (url.pathname === '/api/v1/me' && request.method() === 'GET') return 'me';
    return 'other';
  };
  const onRequest = (request) => {
    if (route(request) === 'create') network.createRequests++;
  };
  const onResponse = (response) => {
    const name = route(response.request());
    if (name === 'other') return;
    if (name === 'create') network.createResponses++;
    network[`${name}Status`] = response.status();
  };
  const onFailed = (request) => {
    if (route(request) === 'create') network.createFailed = true;
  };
  await page.addInitScript(installInputObserver);
  page.on('request', onRequest);
  page.on('response', onResponse);
  page.on('requestfailed', onFailed);
  return {
    stage(name) { lastStage = name; },
    async failed(error) {
      const state = await page.evaluate(readPageState).catch(() => null);
      const evidence = createFailureEvidence(lastStage, error, network, state);
      evidence.lifecycle = lifecycle.snapshot();
      await writeFile('/evidence/publish-create-failure.json', `${JSON.stringify(evidence)}\n`, {
        flag: 'wx', mode: 0o600,
      });
      return evidence.lastStage;
    },
    stop() {
      lifecycle.stop();
      page.off('request', onRequest);
      page.off('response', onResponse);
      page.off('requestfailed', onFailed);
    },
  };
}
