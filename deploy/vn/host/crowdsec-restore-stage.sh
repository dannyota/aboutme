#!/usr/bin/env bash
# Stage only reviewed standalone executables and fixed restore dependencies.
set -euo pipefail
[[ $EUID == 0 && $# == 1 ]] || exit 2
bundle=$(readlink -f "$1")
work=$(mktemp -d /run/crowdsec-restore-stage.XXXXXX)
trap 'rm -rf -- "$work"' EXIT
umask 077
# APT verifies its signed index using the committed key; the archive digest
# also binds the exact reviewed control and payload members. No hooks run.
install -d -m 0755 /etc/apt/keyrings
gpg --dearmor --yes -o "$work/crowdsec.gpg" "$bundle/host/keys/crowdsec.asc" >/dev/null 2>&1
install -o root -g root -m 0644 "$work/crowdsec.gpg" /etc/apt/keyrings/crowdsec.gpg
printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/crowdsec.gpg] https://packagecloud.io/crowdsec/crowdsec/ubuntu noble main' >/etc/apt/sources.list.d/crowdsec.list
apt-get update -qq >/dev/null 2>&1
(cd "$work" && apt-get download crowdsec=1.8.1 >/dev/null 2>&1)
printf '%s  %s\n' d639e87924abe497618795378c13a425e0ffab2bbaecbf50900fae55cbed0ad0 "$work/crowdsec_1.8.1_amd64.deb" | sha256sum --check --status
dpkg-deb -x "$work/crowdsec_1.8.1_amd64.deb" "$work/payload"
for binary in crowdsec cscli; do
  install -o root -g root -m 0755 "$work/payload/usr/bin/$binary" "/usr/bin/$binary"
done
install -d -m 0755 /usr/local/lib/aboutme /etc/crowdsec /etc/crowdsec/hub /var/lib/crowdsec/data
for config in profiles console; do
  install -m 0644 "$bundle/edge/crowdsec/$config.yaml" "/etc/crowdsec/$config.yaml"
done
for helper in crowdsec-db crowdsec-provision crowdsec-cutover-state; do
  install -o root -g root -m 0755 "$bundle/host/$helper.sh" "/usr/local/lib/aboutme/$helper.sh"
done
install -m 0644 "$bundle/host/crowdsec-schema-v1.8.1.tsv" /usr/local/lib/aboutme/crowdsec-schema-v1.8.1.tsv
install -m 0644 "$bundle/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.postgresql
# The candidate has no client section or online client; the fixed initializer
# uses only this file and the already installed PostgreSQL peer policy.
(cd / && sha256sum usr/bin/crowdsec usr/bin/cscli) >"$work/binaries"
install -o root -g root -m 0644 "$work/binaries" /usr/local/lib/aboutme/crowdsec-restore-binaries.sha256
install -d -o root -g root -m 0700 /run/aboutme/crowdsec
