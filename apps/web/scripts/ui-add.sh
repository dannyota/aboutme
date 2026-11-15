#!/usr/bin/env bash
# Generate shadcn-vue primitives with the repository's icon package and lint
# style. Usage: scripts/ui-add.sh <component>...
set -Eeuo pipefail
cd "$(dirname "$0")/.."
if [[ $# -eq 0 ]]; then
  echo 'usage: scripts/ui-add.sh <component>...' >&2
  exit 64
fi
pnpm dlx shadcn-vue@2.8.2 add --yes --overwrite "$@"
# The generator saves these registry dependencies with ranges. Restore the
# frozen exact pins after every run.
pnpm add --save-exact \
  @lucide/vue@1.72.0 @vueuse/core@15.0.0 reka-ui@2.11.0
# The generator imports lucide-vue-next; the repository ships @lucide/vue.
while IFS= read -r file; do
  sed -i "s#from 'lucide-vue-next'#from '@lucide/vue'#g; s#from \"lucide-vue-next\"#from '@lucide/vue'#g" "$file"
done < <(grep -rl 'lucide-vue-next' app/components/ui || true)
pnpm exec eslint --fix app/components/ui
if grep -rq 'lucide-vue-next' app/components/ui; then
  echo 'ui-add: a generated file still imports lucide-vue-next' >&2
  exit 1
fi
