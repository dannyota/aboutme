import { parentPort } from 'node:worker_threads';

const fault = new URL(import.meta.url).searchParams.get('fault');
if (fault === 'malformed') parentPort.postMessage({ nope: true });
if (fault === 'multiple') {
  parentPort.postMessage({ type: 'result', html: '<p>one</p>' });
  parentPort.postMessage({ type: 'result', html: '<p>two</p>' });
}
if (fault === 'oversize') {
  parentPort.postMessage({ type: 'result', html: 'x'.repeat(2_097_153) });
}
if (fault === 'error') throw new Error('worker failure');
if (fault === 'nonzero') process.exitCode = 1;
if (fault === 'none') parentPort.close();
