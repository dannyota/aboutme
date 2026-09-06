import { parentPort } from 'node:worker_threads';

const fault = new URL(import.meta.url).searchParams.get('fault');
if (fault === 'malformed') parentPort?.postMessage({ nope: true });
if (fault === 'duplicate') {
  parentPort?.postMessage({ type: 'result', html: '<p>one</p>' });
  parentPort?.postMessage({ type: 'result', html: '<p>two</p>' });
}
if (fault === 'oversize') {
  parentPort?.postMessage({ type: 'result', html: 'x'.repeat(6_291_457) });
}
if (fault === 'error') throw new Error('raw worker failure');
if (fault === 'nonzero') process.exitCode = 1;
parentPort?.close();
