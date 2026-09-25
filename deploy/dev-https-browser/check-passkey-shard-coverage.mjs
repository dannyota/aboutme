// The CI passkey-browser-proof-coverage job's entry point; the check itself
// is check-shard-coverage.mjs.
import { runShardCoverage } from './check-shard-coverage.mjs';

await runShardCoverage('passkey', process.argv.slice(2));
