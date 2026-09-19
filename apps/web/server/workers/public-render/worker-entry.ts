import { parentPort, workerData } from 'node:worker_threads';

import { decodePublicRenderEnvelope } from '../../utils/public-render/envelope';
import { renderPublicResume } from './render';

// The worker build defines this as the content hash of the resume
// stylesheets (server/utils/print/assets.ts publicStyleVersion).
declare const __ABOUTME_PUBLIC_STYLE_VERSION__: string;

async function main(): Promise<void> {
  try {
    const request = decodePublicRenderEnvelope(JSON.stringify(workerData));
    const html = await renderPublicResume(
      request,
      __ABOUTME_PUBLIC_STYLE_VERSION__,
    );
    parentPort?.postMessage({ type: 'result', html });
    parentPort?.close();
  } catch {
    process.exitCode = 1;
  }
}

void main();
