import { parentPort, workerData } from 'node:worker_threads';

import {
  decodePublicRenderEnvelope,
  type PublicRenderRequest,
} from '../../utils/public-render/envelope';
import type { PublicAssetVersions } from './page-head';
import { renderPublicProject, renderPublicProjectList } from './project-render';
import { renderPublicGate, renderPublicResume } from './render';

// The worker build defines these as content hashes of the resume stylesheets
// and the hydration bundle (server/utils/print/assets.ts).
declare const __ABOUTME_PUBLIC_STYLE_VERSION__: string;
declare const __ABOUTME_PUBLIC_SCRIPT_VERSION__: string;

function render(
  request: PublicRenderRequest,
  versions: PublicAssetVersions,
): Promise<string> {
  switch (request.mode) {
    case 'gate':
      return renderPublicGate(request, versions);
    case 'project-list':
      return renderPublicProjectList(request, versions);
    case 'project':
      return renderPublicProject(request, versions);
    default:
      return renderPublicResume(request, versions);
  }
}

async function main(): Promise<void> {
  try {
    const request = decodePublicRenderEnvelope(JSON.stringify(workerData));
    const versions = {
      style: __ABOUTME_PUBLIC_STYLE_VERSION__,
      script: __ABOUTME_PUBLIC_SCRIPT_VERSION__,
    };
    const html = await render(request, versions);
    parentPort?.postMessage({ type: 'result', html });
    parentPort?.close();
  } catch {
    process.exitCode = 1;
  }
}

void main();
