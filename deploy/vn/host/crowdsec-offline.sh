#!/usr/bin/env bash
# Shared CrowdSec central API opt-out checks. Callers set the paths when a
# test uses a temporary root.

crowdsec_preseed_offline() {
  printf 'crowdsec crowdsec/capi boolean false\n' | debconf-set-selections
}

crowdsec_install_offline() { # install-command package...
  crowdsec_preseed_offline
  "$@"
}

crowdsec_assert_offline() { # base-config local-config credentials
  local base=$1 local_config=$2 credentials=$3 file
  for file in "$base" "$local_config"; do
    [[ -f $file && ! -L $file ]] || {
      echo "CrowdSec config $file is missing or is a symlink" >&2
      return 1
    }
    if grep -Eq '^[[:space:]]+online_client:' "$file"; then
      echo "CrowdSec central API config remains in $file" >&2
      return 1
    fi
  done
  if [[ -e $credentials || -L $credentials ]]; then
    echo "CrowdSec central API credentials remain at $credentials" >&2
    return 1
  fi
}

crowdsec_remove_online() { # base-config local-config credentials
  local base=$1 local_config=$2 credentials=$3 tmp
  tmp=$(mktemp "${base}.XXXXXX")
  awk '
    /^    online_client:/ { skip = 1; next }
    skip && /^      / { next }
    { skip = 0; print }
  ' "$base" >"$tmp"
  install -m 0600 "$tmp" "$base"
  rm -f "$tmp" "$credentials"
  crowdsec_assert_offline "$base" "$local_config" "$credentials"
}
