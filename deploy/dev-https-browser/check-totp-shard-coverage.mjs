import { runShardCoverage } from './check-shard-coverage.mjs';

await runShardCoverage('totp', process.argv.slice(2));
