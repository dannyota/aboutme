import { parentPort } from 'node:worker_threads';

parentPort?.postMessage({ type: 'late-result', html: '<p>late</p>' });
setInterval(() => {}, 1_000);
