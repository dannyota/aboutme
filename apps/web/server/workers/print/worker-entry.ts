import { parentPort, workerData } from 'node:worker_threads';

import {
  isCardEnvelope,
  type PrintJobEnvelope,
} from '../../utils/print/envelope';
import { renderPrintCard, renderPrintResume } from './render';

const envelope = workerData as PrintJobEnvelope;
const html = isCardEnvelope(envelope)
  ? await renderPrintCard(envelope)
  : await renderPrintResume(envelope);
parentPort?.postMessage({ type: 'result', html });
parentPort?.close();
