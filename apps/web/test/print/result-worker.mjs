import { parentPort } from 'node:worker_threads';

parentPort?.postMessage({ type: 'result', html: '<main>Resume</main>' });
parentPort?.close();
