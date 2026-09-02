#!/usr/bin/env bash
# Generate shadcn-vue primitives with the repository's icon package and lint
# style. Usage: scripts/ui-add.sh <component>...
set -Eeuo pipefail
cd "$(dirname "$0")/.."
if [[ $# -eq 0 ]]; then
  echo 'usage: scripts/ui-add.sh <component>...' >&2
  exit 64
fi
# npm exec exports user-level allow-scripts as a project-scoped child setting;
# npm 12 rejects that scope. Remove only the inherited child variables so the
# generator's npm process can read the user's normal config at its valid scope.
npx --yes --package shadcn-vue@2.8.2 -- \
  env -u npm_config_allow_scripts -u NPM_CONFIG_ALLOW_SCRIPTS \
  shadcn-vue add --yes --overwrite "$@"
# The generator saves these registry dependencies with ranges. Restore the
# frozen exact pins after every run.
npm install --save-exact \
  @lucide/vue@1.31.0 @vueuse/core@14.4.0 reka-ui@2.10.4
# The generator imports lucide-vue-next; the repository ships @lucide/vue.
while IFS= read -r file; do
  sed -i "s#from 'lucide-vue-next'#from '@lucide/vue'#g; s#from \"lucide-vue-next\"#from '@lucide/vue'#g" "$file"
done < <(grep -rl 'lucide-vue-next' app/components/ui || true)
npx eslint --fix app/components/ui
if grep -rq 'lucide-vue-next' app/components/ui; then
  echo 'ui-add: a generated file still imports lucide-vue-next' >&2
  exit 1
fi
