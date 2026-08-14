import { parentPort, workerData } from 'node:worker_threads';

import { decodePublicRenderEnvelope } from '../../utils/public-render/envelope';
import { renderPublicResume } from './render';

async function main(): Promise<void> {
  try {
    const request = decodePublicRenderEnvelope(JSON.stringify(workerData));
    const html = await renderPublicResume(request);
    parentPort?.postMessage({ type: 'result', html });
    parentPort?.close();
  } catch {
    process.exitCode = 1;
  }
}

void main();
