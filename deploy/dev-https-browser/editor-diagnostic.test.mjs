import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const {
  STAGE_WORDS,
  classifyFailure,
  classifyPageError,
  classifyRequestFailure,
  classifyStatus,
  classifyURL,
  closedStage,
  installEditorFailureDiagnostic,
  serializeEditorDiagnostic,
} = await import(pathToFileURL(join(here, 'editor-diagnostic.ts')).href);

const ORIGIN = 'https://localhost:20443';
const RESUME_ID = '0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d';
const URL_PLANT = `${ORIGIN}/app/resumes/${RESUME_ID}?token=planted-secret#frag`;
const ERROR_PLANT = 'TimeoutError: locator.press: Timeout 5000ms exceeded. developer@example.invalid planted-secret';
const COUNT_KEYS = ['chunk-load', 'hydration', 'network', 'other'];
const PAGE_KEYS = ['readyState', 'signInHeadingCount', 'signInHeadingVisible', 'urlCategory'];
const REAUTH_KEYS = [...PAGE_KEYS, 'lastNavigationStatusClass', 'mainFrameNavigations'].sort();
const TOP_KEYS = [
  'elapsedMs', 'failureCategory', 'mainPage', 'pageErrors', 'reauthPage',
  'requestFailures', 'schemaVersion', 'stage',
];
const URL_CATEGORIES = [
  'login', 'editor', 'oauth-authorize', 'oauth-callback', 'app-other',
  'about-blank', 'other-origin', 'unavailable',
];
const FAILURE_CATEGORIES = ['assertion-timeout', 'assertion', 'navigation', 'page-closed', 'other'];

const expectTimeout = {
  message: [
    'Error: expect(locator).toBeVisible() failed',
    '',
    "Locator: getByRole('heading', { name: 'Sign in' })",
    'Expected: visible',
    'Timeout: 5000ms',
    'Error: element(s) not found',
    '',
    'Call log:',
    '  - Expect "toBeVisible" with timeout 5000ms',
  ].join('\n'),
};

function keys(value) {
  return Object.keys(value).sort();
}

function isCount(value) {
  return value === 'unavailable' || (Number.isSafeInteger(value) && value >= 0);
}

function assertClosed(output) {
  assert.deepEqual(keys(output), TOP_KEYS);
  assert.equal(output.schemaVersion, 1);
  assert.equal(typeof output.stage, 'string');
  if (output.stage !== 'unavailable') {
    for (const word of output.stage.split('-')) {
      assert.ok(/^[0-9]{1,6}$/.test(word) || STAGE_WORDS.has(word), `open stage word ${word}`);
    }
  }
  assert.ok(FAILURE_CATEGORIES.includes(output.failureCategory));
  assert.deepEqual(keys(output.elapsedMs), ['sinceStageChange', 'sinceTestStart']);
  assert.ok(isCount(output.elapsedMs.sinceTestStart));
  assert.ok(isCount(output.elapsedMs.sinceStageChange));
  for (const [name, expected] of [['mainPage', PAGE_KEYS], ['reauthPage', REAUTH_KEYS]]) {
    const page = output[name];
    assert.deepEqual(keys(page), expected);
    assert.ok(URL_CATEGORIES.includes(page.urlCategory));
    assert.ok(['loading', 'interactive', 'complete', 'unavailable'].includes(page.readyState));
    assert.ok(isCount(page.signInHeadingCount));
    assert.ok([true, false, 'unavailable'].includes(page.signInHeadingVisible));
  }
  assert.ok(isCount(output.reauthPage.mainFrameNavigations));
  assert.ok(['2xx', '3xx', '4xx', '5xx', 'none', 'unavailable']
    .includes(output.reauthPage.lastNavigationStatusClass));
  for (const name of ['pageErrors', 'requestFailures']) {
    assert.deepEqual(keys(output[name]), [...COUNT_KEYS].sort());
    for (const key of COUNT_KEYS) assert.ok(isCount(output[name][key]));
  }
}

function assertNoPlant(serialized) {
  for (const fragment of ['planted', 'https', 'localhost', '20443', '@', RESUME_ID, 'Timeout', 'locator', '?', '/']) {
    assert.ok(!serialized.includes(fragment), `serialized output leaked ${fragment}`);
  }
}

const validInput = () => ({
  schemaVersion: 1,
  stage: 'session-reauth-signin-login-page-loaded-reads401-0-streams-1-loads-0-line-252-reloads-0',
  failureCategory: 'assertion-timeout',
  elapsedMs: { sinceTestStart: 81234, sinceStageChange: 5003 },
  mainPage: {
    urlCategory: 'editor',
    readyState: 'complete',
    signInHeadingCount: 1,
    signInHeadingVisible: true,
  },
  reauthPage: {
    urlCategory: 'login',
    readyState: 'interactive',
    signInHeadingCount: 0,
    signInHeadingVisible: false,
    mainFrameNavigations: 2,
    lastNavigationStatusClass: '2xx',
  },
  pageErrors: { 'chunk-load': 1, hydration: 0, network: 0, other: 2 },
  requestFailures: { 'chunk-load': 0, hydration: 0, network: 3, other: 0 },
});

test('failure errors map to closed categories', () => {
  const cases = [
    [[expectTimeout], 'assertion-timeout'],
    [[{ message: "Error: Timed out 5000ms waiting for expect(locator).toBeVisible()\n\nLocator: getByRole('heading')" }], 'assertion-timeout'],
    [[{ message: 'Error: expect(received).toEqual(expected) // deep equality\n\nCall Log:\n- Timeout 5000ms exceeded while waiting on the predicate' }], 'assertion-timeout'],
    [[{ message: 'Error: expect(received).toBe(expected) // Object.is equality\n\nExpected: 200\nReceived: 503' }], 'assertion'],
    [[Object.assign(new Error('Expected values to be strictly equal'), { name: 'AssertionError' })], 'assertion'],
    [[{ message: `Error: page.goto: net::ERR_CONNECTION_REFUSED at ${ORIGIN}/login\nCall log:\n  - navigating to "${ORIGIN}/login", waiting until "load"` }], 'navigation'],
    [[{ message: 'Error: page.waitForURL: Test timeout of 120000ms exceeded.' }], 'navigation'],
    [[{ message: `Error: page.goto: Navigation to "${ORIGIN}/login" is interrupted by another navigation to "${ORIGIN}/app/resumes"` }], 'navigation'],
    [[{ message: 'Error: locator.isVisible: Target page, context or browser has been closed' }], 'page-closed'],
    [[new Error('headline save did not dispatch')], 'other'],
    [[{ message: 'Test timeout of 120000ms exceeded.' }, expectTimeout], 'assertion-timeout'],
    [[{ message: 'Test timeout of 120000ms exceeded.' }], 'other'],
    [['plain thrown string'], 'other'],
    [[undefined, null, 42], 'other'],
    [[], 'other'],
  ];
  for (const [errors, expected] of cases) {
    assert.equal(classifyFailure(errors), expected, JSON.stringify(errors));
  }
});

test('page URLs map to closed categories', () => {
  const cases = [
    [`${ORIGIN}/login`, 'login'],
    [`${ORIGIN}/login?error=auth_failed`, 'login'],
    [`${ORIGIN}/app/resumes/${RESUME_ID}`, 'editor'],
    [`${ORIGIN}/app/resumes/${RESUME_ID}#photo`, 'editor'],
    [`${ORIGIN}/__uat/oauth/google/authorize?state=planted`, 'oauth-authorize'],
    [`${ORIGIN}/api/v1/auth/google/start?purpose=reauth`, 'oauth-authorize'],
    [`${ORIGIN}/api/v1/auth/google/callback?code=planted&state=planted`, 'oauth-callback'],
    [`${ORIGIN}/app/resumes`, 'app-other'],
    [`${ORIGIN}/`, 'app-other'],
    ['about:blank', 'about-blank'],
    ['https://accounts.google.com/o/oauth2/auth', 'other-origin'],
    ['http://localhost:20443/login', 'other-origin'],
    ['not a url', 'unavailable'],
    ['', 'unavailable'],
  ];
  for (const [url, expected] of cases) {
    assert.equal(classifyURL(url, ORIGIN), expected, url);
  }
});

test('page errors, request failures, and statuses map to closed categories', () => {
  assert.equal(classifyPageError(new TypeError('Failed to fetch dynamically imported module: https://localhost:20443/_nuxt/a.js')), 'chunk-load');
  assert.equal(classifyPageError(new Error('Hydration completed but contains mismatches.')), 'hydration');
  assert.equal(classifyPageError(new TypeError('Failed to fetch')), 'network');
  assert.equal(classifyPageError(new Error('Cannot read properties of undefined')), 'other');
  assert.equal(classifyPageError('not an error'), 'other');
  const failed = (resourceType, errorText, url) => ({ resourceType, errorText, url });
  assert.equal(classifyRequestFailure(failed('script', 'net::ERR_ABORTED', `${ORIGIN}/_nuxt/a.js`), ORIGIN), 'chunk-load');
  assert.equal(classifyRequestFailure(failed('fetch', 'net::ERR_FAILED', `${ORIGIN}/_nuxt/b.mjs`), ORIGIN), 'chunk-load');
  assert.equal(classifyRequestFailure(failed('fetch', 'net::ERR_CONNECTION_RESET', `${ORIGIN}/api/v1/me`), ORIGIN), 'network');
  assert.equal(classifyRequestFailure(failed('fetch', 'net::ERR_ABORTED', `${ORIGIN}/api/v1/me`), ORIGIN), 'other');
  assert.equal(classifyRequestFailure(failed('document', 'net::ERR_BLOCKED_BY_CLIENT', 'https://example.invalid/'), ORIGIN), 'other');
  for (const [status, expected] of [[200, '2xx'], [302, '3xx'], [401, '4xx'], [503, '5xx'], [0, 'none'], [101, 'none']]) {
    assert.equal(classifyStatus(status), expected);
  }
});

test('the stage is written only when every word is closed', () => {
  const closed = 'session-reads-release-start-held-resume-1-events-0-reads401-0-streams-0-loads-0-ended-resume-response-0-failed-1-events-response-0-failed-0-line-122-reloads-0';
  assert.equal(closedStage(closed), closed);
  assert.equal(closedStage('session-reauth-signin-start'), 'session-reauth-signin-start');
  assert.equal(closedStage('photo-upload-saved-session-lost-identity-missing-photo-upload'), 'photo-upload-saved-session-lost-identity-missing-photo-upload');
  assert.equal(closedStage('accessibility-list-reopen-line-133-reloads-1'), 'accessibility-list-reopen-line-133-reloads-1');
  assert.equal(
    closedStage('post-diagnostics-certificate-0-console-1-consolekind-other-consolestage-setup-consoleotherlead-plantedword-pagename-plantedname'),
    'post-diagnostics',
  );
  assert.equal(closedStage('accessibility-editor-light-color-contrast-main-data-testid-planted-line-133-reloads-0'), 'accessibility-editor-light');
  assert.equal(closedStage('accessibility-list-color-contrast-resume-row-resume-id-a'), 'accessibility-list');
  for (const open of [
    'session-reauth-signin-plantedword',
    URL_PLANT,
    ERROR_PLANT,
    `session-reauth-${RESUME_ID}`,
    'session-reauth-signin-start-line-1234567',
    'Session-reauth',
    `${'session-'.repeat(40)}start`,
    '',
  ]) {
    assert.equal(closedStage(open), 'unavailable', open);
  }
});

test('the serializer keeps exactly the allowlisted keys and closed values', () => {
  const input = { ...validInput(), url: URL_PLANT, error: ERROR_PLANT };
  input.mainPage = { ...input.mainPage, url: URL_PLANT, title: ERROR_PLANT };
  input.reauthPage = { ...input.reauthPage, lastNavigationURL: URL_PLANT };
  input.pageErrors = { ...input.pageErrors, [URL_PLANT]: 1 };
  const serialized = serializeEditorDiagnostic(input);
  const output = JSON.parse(serialized);
  assertClosed(output);
  assert.deepEqual(output, validInput());
  assertNoPlant(serialized);
});

test('the serializer drops a planted URL or error message anywhere in its input', () => {
  const plant = (value, depth = 0) => {
    if (typeof value === 'object' && value !== null) {
      const planted = Object.fromEntries(
        Object.entries(value).map(([key, inner]) => [key, plant(inner, depth + 1)]),
      );
      return { ...planted, url: URL_PLANT, message: ERROR_PLANT, [URL_PLANT]: ERROR_PLANT };
    }
    return depth % 2 === 0 ? URL_PLANT : ERROR_PLANT;
  };
  for (const input of [plant(validInput()), plant(validInput(), 1)]) {
    const serialized = serializeEditorDiagnostic(input);
    const output = JSON.parse(serialized);
    assertClosed(output);
    assertNoPlant(serialized);
    assert.equal(output.stage, 'unavailable');
  }
  for (const field of ['stage', 'failureCategory']) {
    for (const planted of [URL_PLANT, ERROR_PLANT]) {
      const serialized = serializeEditorDiagnostic({ ...validInput(), [field]: planted });
      assertClosed(JSON.parse(serialized));
      assertNoPlant(serialized);
    }
  }
  for (const input of [undefined, null, URL_PLANT, [URL_PLANT], { mainPage: URL_PLANT }]) {
    const serialized = serializeEditorDiagnostic(input);
    assertClosed(JSON.parse(serialized));
    assertNoPlant(serialized);
  }
});

function fakePage({ url, readyState, count, visible, closed = false }) {
  const listeners = new Map();
  const mainFrame = { name: 'main' };
  return {
    mainFrame: () => mainFrame,
    on(event, listener) {
      listeners.set(event, [...(listeners.get(event) ?? []), listener]);
      return this;
    },
    emit(event, value) {
      for (const listener of listeners.get(event) ?? []) listener(value);
    },
    isClosed: () => closed,
    url: () => url,
    evaluate: async () => readyState(),
    getByRole: () => ({
      count: async () => count(),
      first: () => ({ isVisible: async () => visible() }),
    }),
  };
}

function navigationResponse(page, status, navigation = true) {
  return {
    status: () => status,
    request: () => ({ isNavigationRequest: () => navigation, frame: () => page.mainFrame() }),
  };
}

test('the writer records a bounded closed diagnostic once', async () => {
  const work = await mkdtemp(join(tmpdir(), 'editor-diagnostic-'));
  try {
    const path = join(work, 'editor-diagnostic.json');
    const clock = { now: 1_000 };
    const state = { editorDiagnosticStage: 'setup' };
    const main = fakePage({
      url: `${ORIGIN}/app/resumes/${RESUME_ID}`,
      readyState: () => new Promise(() => {}),
      count: () => { throw new Error(ERROR_PLANT); },
      visible: () => true,
    });
    const reauth = fakePage({
      url: `${ORIGIN}/login?next=${URL_PLANT}`,
      readyState: () => 'complete',
      count: () => 1,
      visible: () => false,
    });
    const recorder = installEditorFailureDiagnostic(state, main, {
      budgetMs: 50,
      now: () => clock.now,
      origin: ORIGIN,
      path,
    });
    clock.now = 3_000;
    state.editorDiagnosticStage = 'session-destroyed';
    recorder.trackReauthPage(reauth);
    main.emit('pageerror', new TypeError('Failed to fetch dynamically imported module: x'));
    reauth.emit('pageerror', new Error(ERROR_PLANT));
    main.emit('requestfailed', {
      failure: () => ({ errorText: 'net::ERR_CONNECTION_RESET' }),
      resourceType: () => 'fetch',
      url: () => URL_PLANT,
    });
    reauth.emit('framenavigated', reauth.mainFrame());
    reauth.emit('response', navigationResponse(reauth, 500));
    recorder.markReauthSignIn();
    reauth.emit('framenavigated', reauth.mainFrame());
    reauth.emit('framenavigated', { name: 'child' });
    reauth.emit('response', navigationResponse(reauth, 302));
    reauth.emit('response', navigationResponse(reauth, 200));
    reauth.emit('response', navigationResponse(reauth, 404, false));
    clock.now = 6_000;
    state.editorDiagnosticStage = 'session-reauth-signin-login-page-loaded';
    clock.now = 11_000;
    recorder.markFailure();
    clock.now = 11_500;
    recorder.markFailure();
    state.editorDiagnosticStage = `${state.editorDiagnosticStage}-line-252`;
    clock.now = 30_000;
    const started = performance.now();
    await recorder.write([{ message: 'Test timeout of 120000ms exceeded.' }, expectTimeout]);
    assert.ok(performance.now() - started < 1_000, 'collection was not bounded');
    assert.equal((await stat(path)).mode & 0o777, 0o600);
    const serialized = await readFile(path, 'utf8');
    assertNoPlant(serialized);
    const output = JSON.parse(serialized);
    assertClosed(output);
    assert.deepEqual(output, {
      schemaVersion: 1,
      stage: 'session-reauth-signin-login-page-loaded-line-252',
      failureCategory: 'assertion-timeout',
      elapsedMs: { sinceTestStart: 10_000, sinceStageChange: 5_000 },
      mainPage: {
        urlCategory: 'editor',
        readyState: 'unavailable',
        signInHeadingCount: 'unavailable',
        signInHeadingVisible: true,
      },
      reauthPage: {
        urlCategory: 'login',
        readyState: 'complete',
        signInHeadingCount: 1,
        signInHeadingVisible: false,
        mainFrameNavigations: 1,
        lastNavigationStatusClass: '2xx',
      },
      pageErrors: { 'chunk-load': 1, hydration: 0, network: 0, other: 1 },
      requestFailures: { 'chunk-load': 0, hydration: 0, network: 1, other: 0 },
    });
    await recorder.write([new Error('second failure')]);
    assert.equal(await readFile(path, 'utf8'), serialized);

    const existing = join(work, 'existing.json');
    await writeFile(existing, 'kept\n', { mode: 0o600 });
    const closedMain = fakePage({
      url: `${ORIGIN}/login`,
      readyState: () => 'complete',
      count: () => 0,
      visible: () => false,
      closed: true,
    });
    const second = installEditorFailureDiagnostic({ editorDiagnosticStage: 'setup' }, closedMain, {
      budgetMs: 50,
      now: () => clock.now,
      origin: ORIGIN,
      path: existing,
    });
    await second.write([expectTimeout]);
    assert.equal(await readFile(existing, 'utf8'), 'kept\n');

    const fresh = join(work, 'fresh.json');
    const third = installEditorFailureDiagnostic({ editorDiagnosticStage: 'setup' }, closedMain, {
      budgetMs: 50,
      now: () => clock.now,
      origin: ORIGIN,
      path: fresh,
    });
    await third.write([]);
    const unavailable = JSON.parse(await readFile(fresh, 'utf8'));
    assertClosed(unavailable);
    assert.deepEqual(unavailable.mainPage, {
      urlCategory: 'unavailable',
      readyState: 'unavailable',
      signInHeadingCount: 'unavailable',
      signInHeadingVisible: 'unavailable',
    });
    assert.deepEqual(unavailable.reauthPage, {
      urlCategory: 'unavailable',
      readyState: 'unavailable',
      signInHeadingCount: 'unavailable',
      signInHeadingVisible: 'unavailable',
      mainFrameNavigations: 0,
      lastNavigationStatusClass: 'none',
    });
  } finally {
    await rm(work, { force: true, recursive: true });
  }
});

test('the stage vocabulary covers every word the editor stage builders use', () => {
  const read = (name) => readFileSync(join(here, name), 'utf8');
  const words = new Set();
  const add = (text) => {
    for (const word of text.split('-')) if (word !== '') words.add(word);
  };
  const addLiterals = (text, stageShapedOnly = false) => {
    const statics = text.replace(/`(?:[^`$]|\$(?!\{)|\$\{(?:[^{}]|\{[^{}]*\})*\})*`/g, (template) => {
      add(template.slice(1, -1).replace(/\$\{(?:[^{}]|\{[^{}]*\})*\}/g, '-'));
      return '';
    });
    for (const [, operator, literal] of statics.matchAll(/([=!]==\s*)?'([^']*)'/g)) {
      if (operator === undefined && (!stageShapedOnly || /^[a-z0-9-]+$/.test(literal))) add(literal);
    }
  };
  const sources = read('spec-sources.txt').split('\n').filter((name) => /^editor[.-]/.test(name)
    && !['editor-diagnostic.ts', 'editor-diagnostics.ts', 'editor-locale.ts'].includes(name));
  assert.ok(sources.includes('editor.spec.ts') && sources.includes('editor-photo-session.ts'));
  for (const name of sources) {
    const source = read(name);
    for (const [, statement] of source.matchAll(/editorDiagnosticStage\s*=(?!=)([^;]*);/g)) {
      addLiterals(statement);
    }
    for (const [, literal] of source.matchAll(/scanEditorAccessibility\(state, page, '([^']*)'\)/g)) {
      add(literal);
    }
  }
  for (const [name, type] of [['harness-lib.ts', 'SignInWithGoogleStep'], ['editor-fixtures.ts', 'CreateResumeStage']]) {
    const declaration = new RegExp(`export type ${type}\\s*=([^;]*);`).exec(read(name));
    assert.ok(declaration, `missing ${type}`);
    addLiterals(declaration[1]);
  }
  const proofState = read('editor-proof-state.ts');
  for (const helper of ['safeSaveState', 'classifyConflict']) {
    const start = proofState.indexOf(`export function ${helper}(`);
    assert.notEqual(start, -1, `missing ${helper}`);
    addLiterals(proofState.slice(start, proofState.indexOf('\n}\n', start)), true);
  }
  const missing = [...words].filter((word) => !/^[0-9]+$/.test(word) && !STAGE_WORDS.has(word));
  assert.deepEqual(missing, []);
});
