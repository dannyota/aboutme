import { parentPort, workerData } from 'node:worker_threads';

import type { PrintEnvelope } from '../../utils/print/envelope';
import { renderPrintResume } from './render';

const html = await renderPrintResume(workerData as PrintEnvelope);
parentPort?.postMessage({ type: 'result', html });
parentPort?.close();
