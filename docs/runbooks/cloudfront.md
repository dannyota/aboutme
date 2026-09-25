# CloudFront edge

Production is moving its edge from Cloudflare to Amazon CloudFront, in front of
the same EC2 host, under
[ADR 0054](../adr/0054-cloudfront-edge-for-single-host-production.md). See the
[CloudFront edge design](../design/cloudfront-edge.md) for the full request
path, migration steps, and rollback. This runbook covers the parts of that move
that are live today; it grows as later steps land.

## Origin certificate

CloudFront requires a publicly trusted certificate on the origin, with the full
chain, covering the domain it connects to. The origin gets an ACM exportable
public certificate for `aboutme.vn` and `www.aboutme.vn` in `ap-southeast-1`,
ECDSA P-256, exported to SSM. Cloudflare's Full (strict) mode accepts a publicly
trusted certificate, so this certificate serves the Cloudflare edge now and
CloudFront later without a second cutover.

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
certificate, so a partial export or a later `tls.sh origin` stops the next
deploy instead of the next Caddy start. Until the first export writes that
fingerprint, the deploy skips the match check.

Renewal mail arrives 45 days before expiry, so a missed mail still leaves 24
days in which the next deploy stops and names the fix. Nothing alerts when a
renewed certificate was exported but not deployed; the running Caddy keeps the
old certificate until the next deploy or task restart.

## Facts to verify

The [design](../design/cloudfront-edge.md#facts-to-verify) lists facts that
documentation does not settle. Results so far, checked on 2026-09-25 with
read-only calls and the AWS documentation; the rest need the distribution and
are checked before cutover with the real hostnames pinned to it.

| Fact                                | Result                                                                                                                                                                                                           |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. SNI is the forwarded `Host`      | Open. The origin TLS page requires the certificate to match the origin domain or the forwarded `Host` and does not name the SNI value. Caddy enforces SNI and `Host` agreement on the mTLS listener.             |
| 2. IPv6 `CloudFront-Viewer-Address` | Open. The headers page gives only an IPv4 example (`198.51.100.10:46532`). Caddy accepts either form and rejects duplicates.                                                                                     |
| 3. `If-None-Match` reaches origin   | Open; needs the distribution.                                                                                                                                                                                    |
| 4. Error caching TTL 0              | Open; needs the distribution.                                                                                                                                                                                    |
| 5. Origin mTLS through OpenTofu     | Provider 6.64.0 has `custom_origin_config.origin_mtls_config.client_certificate_arn` and `aws_acm_certificate` `options.export`. Pay-as-you-go supports origin mTLS; embedded POPs, WebSockets, and gRPC do not. |
| 6. Security groups change in place  | The host has 1 of 5 allowed groups. The prefix list `com.amazonaws.global.cloudfront.origin-facing` has 46 IPv4 entries against 60 rules per group. The plan must show `~ update in-place`.                      |
| 7. Leaf and chain fit 4 KB          | Not guaranteed: an ACM RSA leaf plus chain in this account is 5,299 bytes. `tls.sh export` stores the chain with the Intelligent-Tiering tier, which picks the advanced tier only when needed.                   |
| 8. Nearby edge from Vietnam         | Open; checked after cutover.                                                                                                                                                                                     |
| 9. Imported certificates are free   | The ACM pricing page lists charges for exportable certificates only (USD 7 per name), none for imported ones. Confirm on the first bill.                                                                         |
