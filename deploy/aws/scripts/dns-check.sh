#!/usr/bin/env bash
# Compares the Route 53 zone for aboutme.vn with what Cloudflare serves, before
# the name servers change at the .vn registrar (docs/runbooks/dns.md). Read
# only: it queries both providers' authoritative servers directly, lists the
# Route 53 zone and its DNSSEC state with the caller's AWS credentials, and
# lists the Cloudflare records with the `cf` CLI.
#
# Usage: AWS_PROFILE=<profile> deploy/aws/scripts/dns-check.sh
#
# DNS_CHECK_CLOUDFLARE_NS  a Cloudflare name server for the zone
#                          (default zahir.ns.cloudflare.com)
# DNS_CHECK_SUBNET         client subnet sent with the CloudFront alias
#                          queries (default 14.160.0.0/24, VNPT in Hanoi)
# DNS_CHECK_CF             0 skips listing the Cloudflare records; without it
#                          a missing `cf` fails the check
#
# Exits 0 when every check passes, 1 when any fails.
set -euo pipefail

zone=aboutme.vn
cf_ns=${DNS_CHECK_CLOUDFLARE_NS:-zahir.ns.cloudflare.com}
subnet=${DNS_CHECK_SUBNET:-14.160.0.0/24}
failures=0

ok() { printf 'ok    %s\n' "$*"; }
fail() {
  printf 'FAIL  %s\n' "$*"
  failures=$((failures + 1))
}
note() { printf 'note  %s\n' "$*"; }
die() {
  printf 'dns-check: %s\n' "$*" >&2
  exit 1
}

for tool in aws dig jq curl openssl base64; do
  command -v "$tool" >/dev/null || die "$tool is required"
done

# One authoritative, non-recursive query. A timeout prints dig's own error
# line, which then fails the comparison instead of stopping the script.
ask() { # server name type [dig options...]
  local server=$1 name=$2 type=$3
  shift 3
  dig +short +norec +time=3 +tries=2 "$@" "@$server" "$name" "$type" || true
}

# Whether a line of the set starts with the given text; end the text with a
# newline to match a whole line.
has_line() { # set prefix
  [[ $'\n'$1$'\n' == *$'\n'"$2"* ]]
}

# Puts one record set in a comparable form: TXT strings joined, names in
# lower case without the trailing dot, lines sorted.
normalize() { # type
  if [[ $1 == TXT ]]; then
    sed -e 's/" "//g' -e 's/^"//' -e 's/"$//'
  else
    tr '[:upper:]' '[:lower:]' | sed -e 's/\.$//' -e 's/\. / /g'
  fi | LC_ALL=C sort -u
}

# ---- The Route 53 zone ----

zone_id=$(aws route53 list-hosted-zones-by-name --dns-name "$zone" --max-items 1 --output json |
  jq -r --arg name "$zone." \
    '.HostedZones[] | select(.Name == $name and .Config.PrivateZone == false) | .Id | sub("^/hostedzone/"; "")')
[[ -n $zone_id ]] || die "no public Route 53 zone named $zone; run tofu apply first"
mapfile -t r53_ns < <(aws route53 get-hosted-zone --id "$zone_id" --output json |
  jq -r '.DelegationSet.NameServers[]')
((${#r53_ns[@]} == 4)) || die "expected 4 Route 53 name servers, got ${#r53_ns[@]}"
printf 'zone %s, name servers %s\n' "$zone_id" "${r53_ns[*]}"

for ns in "${r53_ns[@]}"; do
  if [[ $(ask "$ns" "$zone" SOA) == *awsdns* ]]; then
    ok "$ns answers for $zone"
  else
    fail "$ns gives no SOA for $zone"
  fi
done

# "name type alias" lines, lower case, no trailing dot; NS and SOA belong to
# each provider and are not compared.
r53_sets=$(aws route53 list-resource-record-sets --hosted-zone-id "$zone_id" --output json |
  jq -r '.ResourceRecordSets[] | select(.Type != "NS" and .Type != "SOA") |
    "\(.Name | ascii_downcase | rtrimstr(".")) \(.Type) \(if .AliasTarget then "alias" else "plain" end)"' |
  LC_ALL=C sort -u)

# ---- Every Cloudflare record exists in Route 53 ----

if [[ ${DNS_CHECK_CF:-1} == 0 ]]; then
  note "DNS_CHECK_CF=0: a record that exists only at Cloudflare is not detected"
elif ! command -v cf >/dev/null; then
  fail "cf is not installed, so records that exist only at Cloudflare cannot be listed; set DNS_CHECK_CF=0 to skip"
else
  cf_json=$(cf dns records list -q -z "$zone") || die "cf dns records list failed"
  cf_records=$(jq -r '.[] | "\(.name | ascii_downcase) \(.type)"' <<<"$cf_json" | LC_ALL=C sort -u) ||
    die "cf dns records list did not print a JSON array of records"
  [[ -n $cf_records ]] || die "cf dns records list returned no records"
  while read -r name type; do
    # The apex and www are CNAMEs at Cloudflare and aliases in Route 53.
    if [[ $type == CNAME && ($name == "$zone" || $name == "www.$zone") ]]; then
      name_type="$name A"
    else
      name_type="$name $type"
    fi
    if has_line "$r53_sets" "$name_type "; then
      ok "Cloudflare record $name $type is in Route 53"
    else
      fail "Cloudflare record $name $type is missing from Route 53"
    fi
  done <<<"$cf_records"
fi

# ---- Plain records answer the same from both providers ----

while read -r name type kind; do
  [[ $kind == plain ]] || continue
  cf_answer=$(ask "$cf_ns" "$name" "$type" | normalize "$type")
  same=1
  for ns in "${r53_ns[@]}"; do
    r53_answer=$(ask "$ns" "$name" "$type" | normalize "$type")
    if [[ -z $r53_answer || $r53_answer != "$cf_answer" ]]; then
      fail "$name $type: $ns answers '${r53_answer//$'\n'/, }', Cloudflare '${cf_answer//$'\n'/, }'"
      same=0
    fi
  done
  if ((same == 1)); then
    ok "$name $type matches"
  fi
done <<<"$r53_sets"

# ---- The aliases reach the CloudFront distribution ----

for name in "$zone" "www.$zone"; do
  for type in A AAAA HTTPS; do
    has_line "$r53_sets" "$name $type alias"$'\n' || {
      fail "$name $type is not an alias record"
      continue
    }
    if [[ $type == HTTPS ]]; then
      https=$(ask "${r53_ns[0]}" "$name" HTTPS)
      if [[ $https == *'alpn="h2,h3"'* ]]; then
        ok "$name HTTPS advertises h2 and h3"
      else
        fail "$name HTTPS: '${https//$'\n'/, }' does not advertise h2 and h3"
      fi
      continue
    fi
    mapfile -t addrs < <(ask "${r53_ns[0]}" "$name" "$type" "+subnet=$subnet" | grep -E '^[0-9a-fA-F:.]+$' || true)
    if ((${#addrs[@]} == 0)); then
      fail "$name $type: no addresses"
      continue
    fi
    [[ $type == A ]] || {
      ok "$name AAAA: ${#addrs[@]} addresses"
      continue
    }
    # The certificate, Host, and Via prove the address serves this
    # distribution; x-amz-cf-pop names the edge a viewer in $subnet gets.
    headers=$(curl -sS -o /dev/null -D - --max-time 15 --resolve "$name:443:${addrs[0]}" \
      "https://$name/healthz" 2>&1 | tr -d '\r') || {
      fail "$name via ${addrs[0]}: ${headers##*$'\n'}"
      continue
    }
    status=$(head -n1 <<<"$headers" | awk '{print $2}')
    pop=$(grep -i '^x-amz-cf-pop:' <<<"$headers" | awk '{print $2}' || true)
    if [[ $status =~ ^[23] ]] && grep -qi '^via:.*(CloudFront)' <<<"$headers"; then
      ok "$name via ${addrs[0]}: HTTP $status from CloudFront edge ${pop:-unknown} for $subnet"
    else
      fail "$name via ${addrs[0]}: HTTP ${status:-none} without a CloudFront Via header"
    fi
  done
done

# ---- DNSSEC signing and the DS values for the registrar ----

dnssec=$(aws route53 get-dnssec --hosted-zone-id "$zone_id" --output json)
if [[ $(jq -r '.Status.ServeSignature' <<<"$dnssec") != SIGNING ]]; then
  fail "Route 53 is not signing $zone"
else
  ksk=$(jq -c '[.KeySigningKeys[] | select(.Status == "ACTIVE")] | if length == 1 then .[0] else empty end' <<<"$dnssec")
  if [[ -z $ksk ]]; then
    fail "expected exactly one ACTIVE key-signing key"
  else
    public_key=$(jq -r '.PublicKey' <<<"$ksk")
    digest=$(jq -r '.DigestValue | ascii_upcase' <<<"$ksk")
    for ns in "${r53_ns[@]}"; do
      served=$(ask "$ns" "$zone" DNSKEY | awk '$1 == 257 {k = ""; for (i = 4; i <= NF; i++) k = k $i; print k}')
      if [[ $served == "$public_key" ]]; then
        ok "$ns serves the active key-signing key"
      else
        fail "$ns does not serve the active key-signing key"
      fi
      # dig +short prints an RRSIG as its data, which starts with the type
      # it covers.
      if has_line "$(ask "$ns" "$zone" A +dnssec)" "A "; then
        ok "$ns signs $zone A"
      else
        fail "$ns returns no RRSIG for $zone A"
      fi
    done
    # SHA-256 over the owner name in wire form and the DNSKEY RDATA:
    # flags 257, protocol 3, algorithm 13, public key (RFC 4034, 5.1.4).
    computed=$({
      printf '\x07aboutme\x02vn\x00\x01\x01\x03\x0d'
      base64 -d <<<"$public_key"
    } | openssl dgst -sha256 -r | awk '{print toupper($1)}')
    if [[ $computed == "$digest" ]]; then
      ok "the DS digest matches the active key-signing key"
    else
      fail "the DS digest $digest does not match the active key-signing key ($computed)"
    fi
    printf 'registrar DS: key tag %s, algorithm 13, digest type 2, digest %s\n' \
      "$(jq -r '.KeyTag' <<<"$ksk")" "$digest"
  fi
fi

if ((failures > 0)); then
  printf '%d check(s) failed\n' "$failures"
  exit 1
fi
printf 'all checks passed\n'
