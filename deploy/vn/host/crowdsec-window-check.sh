#!/usr/bin/env bash
# Rejects enabled HTTP or SSH scenarios whose event accumulation can exceed
# 30 minutes. Trigger scenarios have no accumulation window.
set -euo pipefail

scenario_dir=${1:-/etc/crowdsec/scenarios}
[[ -d $scenario_dir ]] || { echo "CrowdSec scenario directory is missing" >&2; exit 1; }
work=$(mktemp)
trap 'rm -f "$work"' EXIT

awk '
  function emit() {
    if (name ~ /(^|\/)http-/ || name ~ /(^|\/)ssh-/ || service == "http" || service == "ssh") {
      print name "|" capacity "|" leakspeed
    }
    name = ""; capacity = ""; leakspeed = ""; service = ""
  }
  FNR == 1 && NR > 1 { emit() }
  /^[[:space:]]*---[[:space:]]*$/ { emit(); next }
  /^[[:space:]]*name:[[:space:]]*/ {
    value = $0; sub(/^[[:space:]]*name:[[:space:]]*/, "", value); gsub(/["'\'' ]/, "", value); name = value
  }
  /^[[:space:]]*capacity:[[:space:]]*/ {
    value = $0; sub(/^[[:space:]]*capacity:[[:space:]]*/, "", value); gsub(/["'\'' ]/, "", value); capacity = value
  }
  /^[[:space:]]*leakspeed:[[:space:]]*/ {
    value = $0; sub(/^[[:space:]]*leakspeed:[[:space:]]*/, "", value); gsub(/["'\'' ]/, "", value); leakspeed = value
  }
  /^[[:space:]]*service:[[:space:]]*/ {
    value = $0; sub(/^[[:space:]]*service:[[:space:]]*/, "", value); gsub(/["'\'' ]/, "", value); service = value
  }
  END { emit() }
' "$scenario_dir"/*.yaml >"$work"

duration_seconds() {
  local value=$1 amount unit
  [[ $value =~ ^([0-9]+)(ms|s|m|h)$ ]] || return 1
  amount=${BASH_REMATCH[1]}
  unit=${BASH_REMATCH[2]}
  case $unit in
  ms) echo 0 ;;
  s) echo "$amount" ;;
  m) echo $((amount * 60)) ;;
  h) echo $((amount * 3600)) ;;
  esac
}

while IFS='|' read -r name capacity leakspeed; do
  [[ -n $name ]] || continue
  [[ -n $leakspeed ]] || continue
  seconds=$(duration_seconds "$leakspeed") || {
    echo "CrowdSec scenario $name has an unsupported leak interval" >&2
    exit 1
  }
  if [[ $capacity =~ ^[0-9]+$ ]]; then
    seconds=$((seconds * capacity))
  elif [[ $capacity != -1 ]]; then
    echo "CrowdSec scenario $name has no bounded capacity" >&2
    exit 1
  fi
  ((seconds <= 1800)) || {
    echo "CrowdSec scenario $name exceeds the 30 minute accumulation limit" >&2
    exit 1
  }
done <"$work"

echo "CrowdSec scenario windows passed"
