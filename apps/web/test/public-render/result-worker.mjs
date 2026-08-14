import { parentPort } from 'node:worker_threads';

parentPort?.postMessage({ type: 'result', html: '<main>Ada Lovelace</main>' });
parentPort?.close();
