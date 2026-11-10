#!/usr/bin/env bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get -o Acquire::Retries=5 update -qq
apt-get install -y -qq ca-certificates gnupg dpkg-dev >/dev/null
install -d -m 0755 /etc/apt/keyrings
install -m 0644 /keys/pgdg.asc /etc/apt/keyrings/postgresql.asc
gpg --dearmor --yes -o /etc/apt/keyrings/crowdsec.gpg /keys/crowdsec.asc
echo 'deb [signed-by=/etc/apt/keyrings/crowdsec.gpg] https://packagecloud.io/crowdsec/crowdsec/ubuntu noble main' >/etc/apt/sources.list.d/crowdsec.list
echo 'deb [signed-by=/etc/apt/keyrings/postgresql.asc] https://apt.postgresql.org/pub/repos/apt noble-pgdg main' >/etc/apt/sources.list.d/pgdg.list
apt-get -o Acquire::Retries=5 update -qq
rm -f /var/cache/apt/archives/*.deb
apt-get install -y -qq --download-only \
  postgresql-common=293.pgdg24.04+1 postgresql-18=18.6-1.pgdg24.04+2 postgresql-client-18=18.6-1.pgdg24.04+2 \
  python3-yaml nftables gettext-base strace crowdsec=1.8.2 crowdsec-firewall-bouncer-nftables=0.0.36 >/dev/null
mkdir -p /out/debs
cp /var/cache/apt/archives/*.deb /out/debs/
cd /out/debs
dpkg-scanpackages --multiversion . /dev/null >Packages 2>/dev/null
gzip -9kn Packages
sha256sum *.deb Packages Packages.gz >SHA256SUMS
ls *.deb | wc -l
du -sh .
