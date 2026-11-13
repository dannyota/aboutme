import { runShardCoverage } from './check-shard-coverage.mjs';

await runShardCoverage('passkey', process.argv.slice(2));
