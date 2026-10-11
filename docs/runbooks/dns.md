# Production DNS

Cloudflare is authoritative for `aboutme.vn` since 2026-10-10 20:56 UTC, on the
Free plan. Every record is DNS-only, with proxying off. The owner edits the
records by hand in Cloudflare. Putting them under OpenTofu remains open.

## Records

| Name                           | Type  | Value or purpose                                                  |
| ------------------------------ | ----- | ----------------------------------------------------------------- |
| `aboutme.vn`, `www.aboutme.vn` | A     | Host floating IP, TTL 300                                         |
| `aboutme.vn`                   | CAA   | `0 issue "letsencrypt.org"`                                       |
| `aboutme.vn`                   | MX    | `smtp.google.com`, Google Workspace mailbox                       |
| `aboutme.vn`                   | TXT   | `v=spf1 include:_spf.bizflycloud.vn include:_spf.google.com ~all` |
| `_dmarc.aboutme.vn`            | TXT   | `v=DMARC1; p=none; rua=mailto:danny@aboutme.vn`                   |
| `dkim._domainkey.aboutme.vn`   | TXT   | Bizfly domain verification key                                    |
| `etrans._domainkey.aboutme.vn` | TXT   | Bizfly mail-signing key                                           |
| `google._domainkey.aboutme.vn` | TXT   | Google Workspace DKIM key                                         |
| Google site-verification name  | CNAME | Google site verification                                          |

Cloudflare also publishes CAA entries for its own certificate authorities
automatically. Keep the Let's Encrypt CAA entry. The [email runbook](email.md)
owns the mail flow and SMTP settings. Keep generated verification values and
DKIM key material out of the repository.

Before replacing or deleting the host, or releasing its floating IP, point the A
records elsewhere. A released address can go to another customer who could then
answer for `aboutme.vn`.

DNS must point at the host before Caddy starts, so Let's Encrypt can validate
its ACME HTTP-01 challenge. DNS-only records send clients directly to Caddy;
Cloudflare provides no HTTP proxy or edge cache for production.

## DNSSEC

Cloudflare signs the zone with algorithm 13 (ECDSAP256SHA256). The owner copies
Cloudflare's DS record from the dashboard to the `.vn` registrar. Signing the
zone and publishing the DS record are separate steps. Do not assume the
registrar step is complete from Cloudflare signing alone.

Check the registrar's DS values against Cloudflare's current values without
copying them into public docs. When changing signing providers, remove the old
DS and wait for its cache lifetime before changing delegation. Publish the new
DS only after resolvers use the new signing provider.

The Route 53 zone, its key-signing key, and its KMS key were deleted on
2026-10-11. Route 53 signing, DNSSEC alarms, and the Route 53 health check are
not production services.

## Checks

Run from a workstation. These commands read DNS and make one HTTPS request:

```sh
dig +short NS aboutme.vn
dig +short A aboutme.vn
dig +short A www.aboutme.vn
dig +short CAA aboutme.vn
dig +short MX aboutme.vn
dig +short TXT aboutme.vn
dig +short TXT _dmarc.aboutme.vn
dig +short TXT dkim._domainkey.aboutme.vn
dig +short TXT etrans._domainkey.aboutme.vn
dig +short TXT google._domainkey.aboutme.vn
dig +short CNAME <google-verification-name>.aboutme.vn
dig +short DS aboutme.vn
dig +dnssec aboutme.vn A @1.1.1.1
delv @1.1.1.1 aboutme.vn A
curl --fail https://aboutme.vn/readyz
```

Use the Google verification name from Cloudflare. Expect Cloudflare name
servers, both A records at the floating IP, and the mail records in the table.
Check TTL 300 for the A records with `dig aboutme.vn A` and
`dig www.aboutme.vn A` against an authoritative name server. After the registrar
publishes the matching DS and caches expire, validating resolvers should set the
`ad` flag and `delv` should report full validation. A missing or mismatched DS
needs the owner's registrar check.

The external site-down alarm belongs to the
[production runbook](production.md#alarms).
