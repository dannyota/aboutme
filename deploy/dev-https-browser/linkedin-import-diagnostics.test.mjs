import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { observeImportNetwork } from './linkedin-import-diagnostics.mjs';

const origin = 'https://localhost:20443';
let phase = 'webkit-not-english';
let milliseconds = 0;
const page = new EventEmitter();
const diagnostic = observeImportNetwork(page, () => phase, () => milliseconds);
const request = (url, navigation = false, errorText = '') => ({
  url: () => url,
  isNavigationRequest: () => navigation,
  failure: () => ({ errorText }),
});

// A console location can be empty while its text identifies the blocked load.
const worker = request(`${origin}/_nuxt/node_modules/pdfjs-dist/legacy/build/pdf.worker.min.mjs?token=private-value`);
page.emit('request', worker);
milliseconds = 10;
page.emit('response', { request: () => worker, status: () => 200 });
phase = 'webkit-timeout';
milliseconds = 20;
page.emit('requestfailed', worker);
diagnostic.console({
  text: () => `Cannot load ${worker.url()} due to access control checks.`,
  location: () => ({ url: '' }),
});
const snapshot = diagnostic.snapshot();
const { priorEvents, ...consoleError } = snapshot.consoleErrors[0];
assert.deepEqual(consoleError, {
  milliseconds: 20, stage: 'timeout', resource: 'pdf-worker',
  lastRequest: 1, kind: 'access-control',
});
assert.deepEqual(priorEvents, snapshot.events);
assert.equal(snapshot.events[0].stage, 'not-english');
assert.equal(snapshot.events[0].status, 200);
assert.equal(snapshot.events[1].event, 'failed');
assert.equal(snapshot.events[1].request, 1);
assert.equal(JSON.stringify(snapshot).includes('private-value'), false);
assert.equal(JSON.stringify(snapshot).includes(origin), false);

// Worker teardown can precede the access-control message for its module load.
const webWorker = new EventEmitter();
webWorker.url = worker.url;
page.emit('worker', webWorker);
webWorker.emit('close');
assert.equal(diagnostic.snapshot().events.at(-2).event, 'worker-open');
assert.equal(diagnostic.snapshot().events.at(-1).event, 'worker-close');
assert.equal(diagnostic.snapshot().events.at(-1).resource, 'pdf-worker');
assert.equal(webWorker.listenerCount('close'), 0);
assert.equal(priorEvents.length, 2);

// Preserve the navigation boundary and failure class without URL or error text.
phase = 'webkit-cap';
const navigation = request(`${origin}/app/resumes?title=private-value`, true);
const refused = request(`${origin}/api/v1/resumes`, false, 'net::ERR_CONNECTION_REFUSED private-value');
page.emit('request', navigation);
page.emit('request', refused);
page.emit('requestfailed', refused);
page.emit('response', { request: () => refused, status: () => 429 });
assert.equal(diagnostic.snapshot().events.at(-3).event, 'navigation');
assert.equal(diagnostic.snapshot().events.at(-2).failure, 'connection-refused');
assert.equal(diagnostic.snapshot().events.at(-1).status, 429);

for (const [errorText, failure] of [
  ['net::ERR_INSUFFICIENT_RESOURCES', 'resources'],
  ['net::ERR_CERT_AUTHORITY_INVALID', 'certificate'],
  ['Load request cancelled', 'cancelled'],
  ['access control checks', 'access-control'],
]) {
  page.emit('requestfailed', request(`${origin}/api/v1/me`, false, errorText));
  assert.equal(diagnostic.snapshot().events.at(-1).failure, failure);
}
page.emit('crash');
assert.equal(diagnostic.snapshot().events.at(-1).event, 'crash');

// Scheme-less known polls and off-origin lookalikes remain distinct.
for (const [url, resource] of [
  ['/localhost:20443/api/v1/capabilities', 'capabilities'],
  [`${origin}/_nuxt/builds/meta/private-value.json`, 'build-meta'],
  ['https://other.invalid/api/v1/capabilities', 'external'],
  [`${origin}/private-value`, 'other-local'],
  ['private-value', 'unknown'],
]) {
  diagnostic.console({
    text: () => `Cannot load ${url} due to access control checks.`,
    location: () => ({ url: '' }),
  });
  assert.equal(diagnostic.snapshot().consoleErrors.at(-1).resource, resource);
}

// Unrestricted messages, stage names, and request errors never reach evidence.
phase = 'private-value';
diagnostic.console({ text: () => 'private-value', location: () => ({ url: 'private-value' }) });
assert.equal(diagnostic.snapshot().consoleErrors.at(-1).stage, 'unknown');
page.emit('requestfailed', request(`${origin}/api/v1/me`, false, 'private-value'));
assert.equal(diagnostic.snapshot().events.at(-1).failure, 'other');
for (let index = 0; index < 100; index++) {
  diagnostic.console({ text: () => 'private-value', location: () => ({ url: '' }) });
  page.emit('requestfailed', request(`${origin}/api/v1/me`, false, 'private-value'));
}
const bounded = diagnostic.snapshot();
assert.equal(bounded.events.length, 24);
assert.equal(bounded.consoleErrors.length, 8);
assert.ok(bounded.droppedEvents > 0);
assert.ok(bounded.droppedConsoleErrors > 0);
assert.ok(Buffer.byteLength(JSON.stringify(bounded)) < 8192);
assert.equal(JSON.stringify(bounded).includes('private-value'), false);

phase = 'webkit-not-linkedin';
milliseconds = 190_000;
const maximumRequest = request(`${origin}/api/v1/capabilities`, false, 'net::ERR_CONNECTION_REFUSED');
for (let index = 0; index < 10_000; index++) page.emit('request', maximumRequest);
for (let index = 0; index < 100; index++) {
  page.emit('requestfailed', maximumRequest);
  diagnostic.console({
    text: () => `Cannot load ${maximumRequest.url()} due to access control checks.`,
    location: () => ({ url: '' }),
  });
}
assert.equal(diagnostic.snapshot().consoleErrors.at(-1).milliseconds, 180_000);
assert.equal(diagnostic.snapshot().consoleErrors.at(-1).lastRequest, 0);
assert.ok(Buffer.byteLength(JSON.stringify(diagnostic.snapshot())) < 8192);

const liveWorker = new EventEmitter();
liveWorker.url = worker.url;
page.emit('worker', liveWorker);
diagnostic.stop();
assert.equal(liveWorker.listenerCount('close'), 0);
for (const event of ['request', 'response', 'requestfailed', 'crash', 'worker']) {
  assert.equal(page.listenerCount(event), 0);
}
