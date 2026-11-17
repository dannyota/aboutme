#!/bin/sh
APT_RETRY_DELAYS=${APT_RETRY_DELAYS:-60 180}
attempt=1
"$@" && exit 0
for delay in $APT_RETRY_DELAYS; do
  echo "apt-retry: attempt $attempt failed; retrying in ${delay}s" >&2
  sleep "$delay"
  attempt=$((attempt + 1))
  "$@" && exit 0
done
echo "apt-retry: giving up after $attempt attempts" >&2
exit 1
