#!/usr/bin/env bash
# Checks dns-check.sh against stubbed aws, dig, curl, and cf answers.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

# Cloudflare's live key-signing key for aboutme.vn and the digest of the DS
# record the .vn registry published for it (key tag 2371): a real pair, so
# the script's own digest computation is checked, not echoed.
ksk_public='mdsswUyr3DPW132mOi8V9xESWE8jTo0dxCjjnopKl+GqJxpVXckHAeF+KkxLbxILfDLUT0rAK9iUzy1L53eKGQ=='
ksk_digest='006A39A4D5419DB32B9BA95D930E0101AA1D8ACD3945C9F0359AF8206CADFEAC'

cat >"$work/bin/stub" <<'STUB'
#!/usr/bin/env bash
# Answers as the command it is named after, for the case in $CASE.
set -euo pipefail
cmd=$(basename "$0")
args=" $* "
case $cmd in
aws)
  case $args in
  *" list-hosted-zones-by-name "*)
    echo '{"HostedZones":[{"Id":"/hostedzone/ZTEST","Name":"aboutme.vn.","Config":{"PrivateZone":false}}]}' ;;
  *" get-hosted-zone "*)
    echo '{"DelegationSet":{"NameServers":["ns-1.awsdns-01.org","ns-2.awsdns-02.net","ns-3.awsdns-03.com","ns-4.awsdns-04.co.uk"]}}' ;;
  *" list-resource-record-sets "*)
    plain='{"Name":"%s","Type":"%s","TTL":300,"ResourceRecords":[]}'
    alias='{"Name":"%s","Type":"%s","AliasTarget":{"DNSName":"d.cloudfront.net."}}'
    # shellcheck disable=SC2059
    printf '{"ResourceRecordSets":[%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s]}\n' \
      "$(printf "$plain" aboutme.vn. NS)" "$(printf "$plain" aboutme.vn. SOA)" \
      "$(printf "$alias" aboutme.vn. A)" "$(printf "$alias" aboutme.vn. AAAA)" \
      "$(printf "$alias" aboutme.vn. HTTPS)" "$(printf "$alias" www.aboutme.vn. HTTPS)" \
      "$(printf "$alias" www.aboutme.vn. A)" "$(printf "$alias" www.aboutme.vn. AAAA)" \
      "$(printf "$plain" aboutme.vn. MX)" "$(printf "$plain" aboutme.vn. TXT)" \
      "$(printf "$plain" aboutme.vn. CAA)" "$(printf "$plain" google._domainkey.aboutme.vn. TXT)" ;;
  *" get-dnssec "*)
    digest=$KSK_DIGEST signing=SIGNING
    [[ $CASE == bad-digest ]] && digest=${digest/006A/006B}
    [[ $CASE == not-signing ]] && signing=NOT_SIGNING
    printf '{"Status":{"ServeSignature":"%s"},"KeySigningKeys":[{"Status":"ACTIVE","KeyTag":2371,"PublicKey":"%s","DigestValue":"%s"}]}\n' \
      "$signing" "$KSK_PUBLIC" "$digest" ;;
  *) echo "unexpected aws $*" >&2; exit 1 ;;
  esac ;;
dig)
  server= dnssec=0
  for a in "$@"; do
    case $a in @*) server=${a#@} ;; +dnssec) dnssec=1 ;; esac
  done
  name=${*: -2:1} type=${*: -1}
  if [[ $CASE == ns-timeout && $server == ns-3.awsdns-03.com && $type != SOA ]]; then
    echo ';; communications error to 192.0.2.3#53: timed out'
    exit 9
  fi
  provider=route53
  [[ $server == *cloudflare* ]] && provider=cloudflare
  spf='"v=spf1 include:_spf.google.com ~all"'
  [[ $CASE == txt-diff && $provider == cloudflare ]] && spf='"v=spf1 include:_spf.google.com -all"'
  case "$name $type" in
  "aboutme.vn SOA") echo "$server. awsdns-hostmaster.amazon.com. 1 7200 900 1209600 86400" ;;
  "aboutme.vn MX") echo '1 smtp.google.com.' ;;
  "aboutme.vn TXT") echo "$spf" ;;
  "aboutme.vn CAA") echo '0 issue "amazon.com"' ;;
  "google._domainkey.aboutme.vn TXT")
    # The providers split the long value at different points.
    if [[ $provider == cloudflare ]]; then echo '"v=DKIM1;k=rsa;p=AB" "CDEF"'; else echo '"v=DKIM1;k=rsa;p=ABCD" "EF"'; fi ;;
  "aboutme.vn A" | "www.aboutme.vn A")
    printf '18.160.0.1\n18.160.0.2\n'
    if ((dnssec == 1)); then
      echo 'A 13 2 60 20261001000000 20260925000000 1 aboutme.vn. c2lnbmF0dXJl'
    fi ;;
  "aboutme.vn AAAA" | "www.aboutme.vn AAAA") echo '2600:9000::1' ;;
  "aboutme.vn HTTPS" | "www.aboutme.vn HTTPS") echo '1 . alpn="h2,h3"' ;;
  "aboutme.vn DNSKEY")
    key=$KSK_PUBLIC
    [[ $CASE == dnskey-mismatch && $server == ns-2.awsdns-02.net ]] && key=${key/mdss/mdsT}
    printf '257 3 13 %s %s\n' "${key:0:56}" "${key:56}"
    echo '256 3 13 b3RoZXI=' ;;
  esac ;;
curl)
  if [[ $CASE == not-cloudfront ]]; then
    printf 'HTTP/2 200\r\nserver: other\r\n\r\n'
  else
    printf 'HTTP/2 200\r\nvia: 1.1 abc.cloudfront.net (CloudFront)\r\nx-amz-cf-pop: HAN50-P1\r\n\r\n'
  fi ;;
cf)
  if [[ $CASE == cf-bad-json ]]; then
    echo 'Error: not logged in'
    exit 0
  fi
  extra=
  [[ $CASE == cf-extra ]] && extra=',{"name":"bounce.aboutme.vn","type":"MX"}'
  printf '[{"name":"aboutme.vn","type":"CNAME"},{"name":"www.aboutme.vn","type":"CNAME"},{"name":"aboutme.vn","type":"MX"},{"name":"aboutme.vn","type":"TXT"},{"name":"aboutme.vn","type":"CAA"},{"name":"google._domainkey.aboutme.vn","type":"TXT"}%s]\n' "$extra" ;;
esac
exit 0
STUB
chmod +x "$work/bin/stub"
for cmd in aws dig curl cf; do
  ln -s stub "$work/bin/$cmd"
done

run_case() { # name want-exit [expected output line]
  local name=$1 want=$2 line=${3:-} got=0
  CASE=$name KSK_PUBLIC=$ksk_public KSK_DIGEST=$ksk_digest PATH="$work/bin:$PATH" \
    bash "$here/dns-check.sh" >"$work/$name.out" 2>&1 || got=$?
  if ((got != want)); then
    echo "$name: exit $got, want $want" >&2
    cat "$work/$name.out" >&2
    exit 1
  fi
  if [[ -n $line ]] && ! grep -qF -- "$line" "$work/$name.out"; then
    echo "$name: missing output line: $line" >&2
    cat "$work/$name.out" >&2
    exit 1
  fi
}

run_case pass 0 'all checks passed'
for line in \
  'ok    google._domainkey.aboutme.vn TXT matches' \
  'ok    Cloudflare record aboutme.vn CNAME is in Route 53' \
  'ok    the DS digest matches the active key-signing key' \
  'registrar DS: key tag 2371, algorithm 13, digest type 2, digest 006A39A4' \
  'ok    aboutme.vn via 18.160.0.1: HTTP 200 from CloudFront edge HAN50-P1' \
  'ok    www.aboutme.vn HTTPS advertises h2 and h3'; do
  grep -qF -- "$line" "$work/pass.out" || {
    echo "pass: missing output line: $line" >&2
    cat "$work/pass.out" >&2
    exit 1
  }
done
run_case txt-diff 1 'FAIL  aboutme.vn TXT: ns-1.awsdns-01.org answers'
run_case cf-extra 1 'FAIL  Cloudflare record bounce.aboutme.vn MX is missing from Route 53'
run_case not-cloudfront 1 'without a CloudFront Via header'
run_case bad-digest 1 'does not match the active key-signing key'
run_case not-signing 1 'FAIL  Route 53 is not signing aboutme.vn'
run_case dnskey-mismatch 1 'FAIL  ns-2.awsdns-02.net does not serve the active key-signing key'
run_case cf-bad-json 1 'did not print a JSON array of records'
run_case ns-timeout 1 'FAIL  aboutme.vn MX: ns-3.awsdns-03.com answers'
echo "dns-check tests passed"
