# CloudFront edge

Production serves `https://aboutme.vn` through Amazon CloudFront, in front of
the same EC2 host, under
[ADR 0054](../adr/0054-cloudfront-edge-for-single-host-production.md). See the
[CloudFront edge design](../design/cloudfront-edge.md) for the full request path
and the return path to Cloudflare. Cloudflare answers DNS only; see the
[production runbook's Cloudflare DNS section](production.md#cloudflare-dns).
This runbook covers the origin certificate, the client certificate, and the
checks that run around a deploy or an edge change.

## Origin certificate

CloudFront requires a publicly trusted certificate on the origin, with the full
chain, covering the domain it connects to. The origin gets an ACM exportable
public certificate for `aboutme.vn` and `www.aboutme.vn` in `ap-southeast-1`,
ECDSA P-256, exported to SSM.

`deploy/aws/modules/edge` provisions the certificate with DNS validation, but
does not wait for it to issue: OpenTofu never adds the validation records
itself.

### Owner step: add the validation records

After `tofu apply` creates the certificate, add its DNS validation records at
Cloudflare, as DNS-only (grey cloud), not proxied:

```sh
tofu output -json certificate_validation_records
```

Each entry is `{name, type, value}`; create a `CNAME` record for the exact
`name` and `value` given. These same records also validate the viewer
certificate added in a later step, so only one set of DNS-only records is ever
needed for both.

### Export and deploy

Once ACM shows the certificate as issued, export it and deploy the live tag:

```sh
deploy/aws/scripts/tls.sh export
deploy/aws/scripts/deploy.sh <tag>
```

`tls.sh export` finds the exportable, issued certificate for `aboutme.vn`,
exports it with a one-time passphrase inside a private tmpfs directory, verifies
the key matches the certificate, that both names are covered, and that at least
21 days remain, then stores the key as a SecureString at
`/aboutme/prod/tls/origin-key` and the full chain as a String at
`/aboutme/prod/tls/origin-cert`. It prints the certificate's expiry, the stored
chain's byte count, and the SSM parameter tier, never the key itself.

### Renewal

ACM renews the certificate automatically, 45 days before its 198-day expiry. Two
EventBridge rules, `aboutme-prod-origin-cert-renewed` and
`aboutme-prod-origin-cert-renewal-blocked`, send mail through the existing
alerts topic: one when the renewal succeeds, one when it needs manual action
(for example the validation records went missing). On the renewal mail, run the
export and deploy steps above again.

### Deploy guard

`deploy.sh` refuses a normal deploy, `--first-deploy`, or `--rollback` when the
certificate stored at `/aboutme/prod/tls/origin-cert`, which every Caddy task
started by the deploy reads, has fewer than 21 days left, cannot be read, or
does not match the key: `tls.sh` stores the SHA-256 of the key's public key at
`/aboutme/prod/tls/origin-key-sha256`, written before the key and the
certificate, so a partial export stops the next deploy instead of the next Caddy
start. When no fingerprint is stored, the deploy skips the match check.

Renewal mail arrives 45 days before expiry, so a missed mail still leaves 24
days in which the next deploy stops and names the fix. Nothing alerts when a
renewed certificate was exported but not deployed; the running Caddy keeps the
old certificate until the next deploy or task restart.

## Origin mTLS client certificate

CloudFront authenticates to Caddy's CloudFront listener with a client
certificate from our own CA: ECDSA P-256, the `clientAuth` extended key usage,
imported into ACM in `us-east-1`. OpenTofu's `edge` module looks it up by domain
name `cloudfront-origin.aboutme.vn` and wires its ARN into the distribution's
origin mTLS config, so it never touches OpenTofu state as a managed resource.

```sh
deploy/aws/scripts/tls.sh client-ca
deploy/aws/scripts/tls.sh client-import
```

`tls.sh client-ca` generates a new CA (its key is discarded) and a client
certificate and key signed by it, in the per-user tmpfs. It adds the CA
certificate to the trust pool stored at
`/aboutme/prod/tls/cloudfront-client-ca`, keeping any CA already there: Caddy
must trust the old and the new CA at once during a rotation.
`tls.sh client-import` imports the client certificate and key into ACM and
deletes the tmpfs files; `tls.sh forget-client` deletes them without importing.

On the first build, run `client-ca` then `client-import`, then `tofu apply`,
since nothing is deployed yet to trust. On a rotation, the order reverses:
`client-ca`, then deploy the live tag so Caddy trusts the new CA alongside the
old one, then `client-import`, which reimports under the same ACM certificate
ARN so the distribution's configuration never changes. The old CA stays in the
trust pool, and Caddy keeps trusting it, until someone stores the parameter
again without it and deploys.

An EventBridge rule, `aboutme-prod-client-cert-expiring`, sends mail through the
`us-east-1` alerts topic 45 days before the imported certificate expires. There
is no automatic renewal for an imported certificate; the mail is the only signal
to rotate.

## Deploy checks

`deploy.sh` runs a distribution preflight before it changes anything else: the
live distribution for `aboutme.vn` must exist, and its origin must match the
host's HTTPS port (8443), protocol policy, mTLS client certificate, and domain
name. After a deploy, the smoke checks require HSTS, a `Via` header naming
CloudFront (`(CloudFront)`), and no `CF-Ray` (Cloudflare's proxy passes
CloudFront's `Via` along) on `https://aboutme.vn/`, and require that a direct
request to the Elastic IP on 443 and 8443 time out. The maintenance smoke goes
through CloudFront too. The checks through CloudFront retry any failed answer,
such as a 502 or 504, a bounded number of times; the direct-origin check never
retries.

## Edge checks

After a change to the distribution, the web ACL, or the Caddy CloudFront
listener, run the [design test plan](../design/cloudfront-edge.md#test-plan)
items against the public hostnames `aboutme.vn` and `www.aboutme.vn`. Evidence
goes under `.dev/prod-checks/cloudfront/`.

To check one edge address in isolation, pin the real hostnames to it so cookies,
CSRF origin checks, and passkeys, which all depend on the real hostname, stay
valid:

```sh
dig +short "$(tofu output -raw distribution_domain_name)"
curl --resolve aboutme.vn:443:<address> https://aboutme.vn/healthz
```

For a browser check, start Chromium with
`--host-resolver-rules="MAP aboutme.vn <address>, MAP www.aboutme.vn <address>"`.

Record the result of the open facts in the [facts table](#facts-to-verify): SNI
on the origin connection, the IPv6 `CloudFront-Viewer-Address` format, whether
viewer `If-None-Match` reaches the origin, whether error caching TTL 0 keeps a
maintenance 503 off `/_nuxt/*`, the nearby-edge fact from Vietnam, and the
reimport fact at the first client certificate rotation.

## Returning to Cloudflare

Serving through Cloudflare again needs the earlier Caddy 443 listener and the
Cloudflare security group and range-check code back from Git history, a reviewed
`tofu apply`, and a deploy. At Cloudflare, turn Universal SSL on and remove the
CAA record before proxying the apex and `www` records again, since a proxied
record fails TLS while Universal SSL is off and CAA blocks Cloudflare's edge
certificate. Authenticated Origin Pulls then needs a new origin pull
certificate; the [design](../design/cloudfront-edge.md) covers the full rollback
path.

## Facts to verify

The [design](../design/cloudfront-edge.md#facts-to-verify) lists facts that
documentation does not settle. Results so far, checked on 2026-09-25 with
read-only calls and the AWS documentation; the rest are checked against the live
distribution with the real hostnames.

| Fact                                | Result                                                                                                                                                                                                           |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. SNI is the forwarded `Host`      | Open. The origin TLS page requires the certificate to match the origin domain or the forwarded `Host` and does not name the SNI value. Caddy enforces SNI and `Host` agreement on the mTLS listener.             |
| 2. IPv6 `CloudFront-Viewer-Address` | Open. The headers page gives only an IPv4 example (`198.51.100.10:46532`). Caddy accepts either form and rejects duplicates.                                                                                     |
| 3. `If-None-Match` reaches origin   | Open; needs the distribution.                                                                                                                                                                                    |
| 4. Error caching TTL 0              | Open; needs the distribution.                                                                                                                                                                                    |
| 5. Origin mTLS through OpenTofu     | Provider 6.64.0 has `custom_origin_config.origin_mtls_config.client_certificate_arn` and `aws_acm_certificate` `options.export`. Pay-as-you-go supports origin mTLS; embedded POPs, WebSockets, and gRPC do not. |
| 6. Security groups change in place  | The host has 1 of 5 allowed groups. The prefix list `com.amazonaws.global.cloudfront.origin-facing` has 46 IPv4 entries against 60 rules per group. The plan must show `~ update in-place`.                      |
| 7. Leaf and chain fit 4 KB          | Not guaranteed: an ACM RSA leaf plus chain in this account is 5,299 bytes. `tls.sh export` stores the chain with the Intelligent-Tiering tier, which picks the advanced tier only when needed.                   |
| 8. Nearby edge from Vietnam         | Open; check with the origin latency poll from Vietnam.                                                                                                                                                           |
| 9. Imported certificates are free   | The ACM pricing page lists charges for exportable certificates only (USD 7 per name), none for imported ones. Confirm on the first bill.                                                                         |
| 10. Reimport keeps origin mTLS      | Open. The rotation assumes CloudFront presents a client certificate reimported under the same ARN without a distribution change; check at the first rotation.                                                    |
