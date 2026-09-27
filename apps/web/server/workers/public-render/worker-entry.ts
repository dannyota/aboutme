import { parentPort, workerData } from 'node:worker_threads';

import { decodePublicRenderEnvelope } from '../../utils/public-render/envelope';
import { renderPublicGate, renderPublicResume } from './render';

// The worker build defines these as content hashes of the resume stylesheets
// and the hydration bundle (server/utils/print/assets.ts).
declare const __ABOUTME_PUBLIC_STYLE_VERSION__: string;
declare const __ABOUTME_PUBLIC_SCRIPT_VERSION__: string;

async function main(): Promise<void> {
  try {
    const request = decodePublicRenderEnvelope(JSON.stringify(workerData));
    const versions = {
      style: __ABOUTME_PUBLIC_STYLE_VERSION__,
      script: __ABOUTME_PUBLIC_SCRIPT_VERSION__,
    };
    const html = request.mode === 'gate'
      ? await renderPublicGate(request, versions)
      : await renderPublicResume(request, versions);
    parentPort?.postMessage({ type: 'result', html });
    parentPort?.close();
  } catch {
    process.exitCode = 1;
  }
}

void main();
