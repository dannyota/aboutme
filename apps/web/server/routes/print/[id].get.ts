import { defineEventHandler } from 'h3';

// @ts-expect-error Nitro Rollup emits this module at build time.
import workerUrl from '#print-worker-url';
import { createPrintHandler } from '../../utils/print/handler';
import { directPrintTransport } from '../../utils/print/redemption';
import { runPrintWorker } from '../../utils/print/runner';

export default defineEventHandler((event) => {
  const runtimeConfig = useRuntimeConfig(event);
  return createPrintHandler({
    origin: runtimeConfig.printOrigin,
    transport: directPrintTransport,
    run: runPrintWorker,
    workerUrl,
  })(event);
});
