const ORIGIN = 'https://localhost:20443';
const STAGES = [
  'sign-in', 'not-linkedin', 'not-english', 'timeout', 'cap', 'done',
  'navigate-list', 'ready', 'open-dialog', 'blank-selected', 'title-filled',
  'submitted', 'response-created', 'response-cap-exceeded', 'response-other',
];
const EVENT_LIMIT = 24;
const CONSOLE_LIMIT = 8;

// Public evidence keeps fixed resource names, never URLs or browser text.
function resourceOf(value) {
  let url;
  try {
    const token = value.startsWith('/localhost:20443/')
      ? `https:/${value}` : value;
    url = new URL(token);
  } catch {
    return 'unknown';
  }
  if (url.protocol === 'wss:') {
    url.protocol = 'https:';
    if (url.origin !== ORIGIN) return 'external';
    return url.pathname.startsWith('/_nuxt/') ? 'nuxt-socket' : 'local-socket';
  }
  if (url.origin !== ORIGIN) return 'external';
  const path = url.pathname;
  if (path === '/api/v1/capabilities') return 'capabilities';
  if (path === '/api/v1/me') return 'me';
  if (path === '/api/v1/resumes') return 'resumes';
  if (path.startsWith('/api/v1/resumes/')) return 'resume';
  if (path.startsWith('/_nuxt/builds/meta/')) return 'build-meta';
  if (path.startsWith('/_nuxt/')) {
    return /\/pdf\.worker(?:\.min)?\.mjs$/u.test(path) ? 'pdf-worker' : 'nuxt-asset';
  }
  if (path === '/app/import/linkedin') return 'import-page';
  if (path === '/app/resumes') return 'list-page';
  if (path.startsWith('/app/resumes/')) return 'editor-page';
  if (path === '/login') return 'login-page';
  return 'other-local';
}

function failureOf(text) {
  if (/Network process crashed/u.test(text)) return 'network-process-crash';
  if (/ERR_INSUFFICIENT_RESOURCES/u.test(text)) return 'resources';
  if (/ERR_CERT|certificate/iu.test(text)) return 'certificate';
  if (/ERR_CONNECTION_REFUSED/u.test(text)) return 'connection-refused';
  if (/abort|cancel/iu.test(text)) return 'cancelled';
  if (/access control/iu.test(text)) return 'access-control';
  return 'other';
}

export function observeImportNetwork(page, getStage, clock = () => performance.now()) {
  const started = clock();
  const events = [];
  const consoleErrors = [];
  const requests = new WeakMap();
  const urls = new Map();
  const workers = new Map();
  const socketHandlers = new Map();
  const sockets = { opened: 0, nuxt: 0, connected: 0, fullReloads: 0 };
  const increment = (key) => { sockets[key] = Math.min(10_000, sockets[key] + 1); };
  let droppedSockets = 0;
  let requestCount = 0;
  let droppedEvents = 0;
  let droppedConsoleErrors = 0;
  const stamp = () => {
    const phase = getStage().replace(/^(?:chromium|webkit)-/u, '');
    return {
      milliseconds: Math.max(0, Math.min(180_000, Math.round(clock() - started))),
      stage: STAGES.includes(phase) ? phase : 'unknown',
    };
  };
  const append = (record) => {
    if (events.length === EVENT_LIMIT) {
      events.shift();
      droppedEvents++;
    }
    events.push(record);
  };
  const describe = (request) => ({
    request: requests.get(request) ?? 0,
    resource: resourceOf(request.url()),
  });
  const onRequest = (request) => {
    const id = requestCount < 10_000 ? ++requestCount : 0;
    requests.set(request, id);
    urls.set(request.url(), id);
    if (urls.size > 256) urls.delete(urls.keys().next().value);
    if (request.isNavigationRequest()) {
      append({ ...stamp(), event: 'navigation', ...describe(request) });
    }
  };
  const onResponse = (response) => {
    const description = describe(response.request());
    const status = response.status();
    if (status < 400 && !['capabilities', 'me', 'resumes', 'pdf-worker'].includes(description.resource)) return;
    append({ ...stamp(), event: 'response', ...description, status });
  };
  const onFailed = (request) => append({
    ...stamp(), event: 'failed', ...describe(request),
    failure: failureOf(request.failure()?.errorText ?? ''),
  });
  const onCrash = () => append({ ...stamp(), event: 'crash' });
  const onWorker = (worker) => {
    const resource = resourceOf(worker.url());
    append({ ...stamp(), event: 'worker-open', resource });
    const onClose = () => {
      append({ ...stamp(), event: 'worker-close', resource });
      worker.off('close', onClose);
      workers.delete(worker);
    };
    workers.set(worker, onClose);
    worker.on('close', onClose);
  };
  const onSocket = (socket) => {
    const resource = resourceOf(socket.url());
    increment('opened');
    if (resource === 'nuxt-socket') increment('nuxt');
    append({ ...stamp(), event: 'socket-open', resource });
    if (socketHandlers.size === 8) {
      droppedSockets++;
      return;
    }
    const onFrame = ({ payload }) => {
      if (!['nuxt-socket', 'local-socket'].includes(resource)
        || typeof payload !== 'string' || payload.length > 4096) return;
      let type;
      try { type = JSON.parse(payload)?.type; } catch { return; }
      if (type !== 'connected' && type !== 'full-reload') return;
      increment(type === 'connected' ? 'connected' : 'fullReloads');
      append({ ...stamp(), event: `vite-${type}`, resource });
    };
    const onError = (text) => append({
      ...stamp(), event: 'socket-error', resource, failure: failureOf(text),
    });
    const onClose = () => {
      append({ ...stamp(), event: 'socket-close', resource });
      for (const [event, handler] of Object.entries(handlers)) socket.off(event, handler);
      socketHandlers.delete(socket);
    };
    const handlers = { framereceived: onFrame, socketerror: onError, close: onClose };
    socketHandlers.set(socket, handlers);
    for (const [event, handler] of Object.entries(handlers)) socket.on(event, handler);
  };
  const handlers = {
    request: onRequest, response: onResponse, requestfailed: onFailed,
    crash: onCrash, worker: onWorker, websocket: onSocket,
  };
  for (const [event, handler] of Object.entries(handlers)) page.on(event, handler);
  return {
    console(message) {
      const text = message.text();
      const accessControlToken = /(\S+) due to access control checks\.$/u.exec(text)?.[1];
      const socketToken = /WebSocket connection to '([^']+)' failed:/u.exec(text)?.[1];
      const token = accessControlToken ?? socketToken;
      const target = token ?? message.location().url;
      if (consoleErrors.length === CONSOLE_LIMIT) {
        consoleErrors.shift();
        droppedConsoleErrors++;
      }
      // Console text names a URL, not a unique request. Keep its last observed
      // request and the nearby events before later cap checks replace them.
      consoleErrors.push({
        ...stamp(), resource: resourceOf(target), lastRequest: urls.get(target) ?? 0,
        kind: accessControlToken !== undefined ? 'access-control'
          : socketToken !== undefined ? failureOf(text) : 'other',
        priorEvents: events.slice(-3),
      });
    },
    snapshot() {
      return {
        events: events.slice(), consoleErrors: consoleErrors.slice(),
        sockets: { ...sockets }, droppedEvents, droppedConsoleErrors, droppedSockets,
      };
    },
    stop() {
      for (const [event, handler] of Object.entries(handlers)) page.off(event, handler);
      for (const [worker, handler] of workers) worker.off('close', handler);
      workers.clear();
      for (const [socket, handlers] of socketHandlers) {
        for (const [event, handler] of Object.entries(handlers)) socket.off(event, handler);
      }
      socketHandlers.clear();
    },
  };
}
