// @ts-expect-error Nitro Rollup emits this module at build time.
import workerUrl from '#public-render-worker-url';

import { createPublicRenderHandler } from '../../utils/public-render/handler';
import { runPublicRenderWorker } from '../../utils/public-render/runner';

export default createPublicRenderHandler({
  run: runPublicRenderWorker,
  workerUrl,
});
