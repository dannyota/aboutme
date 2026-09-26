# DNS

Status: **prepared**. OpenTofu's `deploy/aws/modules/dns` builds a public Route
53 hosted zone for `aboutme.vn` with every record Cloudflare serves today, and
signs it with DNSSEC. Cloudflare stays authoritative until the owner changes the
name servers at the `.vn` registrar, in the order below.

The move exists for the apex. Cloudflare flattens the apex CNAME to the
CloudFront distribution itself, so CloudFront picks an edge near Cloudflare's
resolver, not near the viewer: from Vietnam through `8.8.8.8` the apex landed on
Marseille (first byte 1.0 s), while `www`, a plain CNAME, landed on Hanoi. A
Route 53 alias record answers with the distribution's addresses for the querying
resolver's location, or for the client subnet the resolver sends. The subnet
behavior is not stated in the AWS documentation for aliases, so `dns-check.sh`
measures it before the switch.

## Zone

Every record except the aliases has TTL 300. The zone's own NS record has TTL
3600, so once resolvers reach Route 53 they recheck its name servers hourly. The
`.vn` delegation keeps its own TTL of 43200, which governs how fast a change at
the registrar reaches resolvers.

| Record                                         | Type                 | Source                                                           |
| ---------------------------------------------- | -------------------- | ---------------------------------------------------------------- |
| `aboutme.vn`, `www.aboutme.vn`                 | Alias A, AAAA, HTTPS | The CloudFront distribution (`module.edge`)                      |
| `aboutme.vn`                                   | CAA                  | `0 issue "amazon.com"`, fixed                                    |
| `aboutme.vn`                                   | MX, TXT              | Google Workspace MX and SPF, fixed ([email runbook](email.md))   |
| `_dmarc.aboutme.vn`                            | TXT                  | DMARC, fixed                                                     |
| `bounce.aboutme.vn`                            | MX, TXT              | SES custom MAIL FROM, fixed                                      |
| `<token>._domainkey.aboutme.vn` (three)        | CNAME                | SES Easy DKIM tokens, read from the SES identity                 |
| `google._domainkey.aboutme.vn`                 | TXT                  | `google_workspace.dkim_txt` in `prod.tfvars`                     |
| `<label>.aboutme.vn`                           | CNAME                | Google domain verification, `google_workspace` in `prod.tfvars`  |
| `_<hash>.aboutme.vn`, `_<hash>.www.aboutme.vn` | CNAME                | ACM validation, from the origin certificate's validation options |

The ACM records come from the certificate resource, so they stay correct and
both certificates keep renewing on their own. ACM gives the origin and viewer
certificates the same records. Generated values stay out of this public
repository: Google's live in the ignored `prod.tfvars`, and AWS's only in state.

Cost: USD 0.50 per month for the zone and USD 0.40 per million standard queries;
alias queries to CloudFront are free ([Route 53 pricing][r53-price]). DNSSEC
adds the KMS key, USD 1 per month, and USD 0.15 per 10,000 signing requests
([KMS pricing][kms-price]).

## DNSSEC

Route 53 signs the zone from the first apply with a key-signing key (KSK) on an
asymmetric `ECC_NIST_P256` KMS key in `us-east-1`, as Route 53 requires ([KMS
key requirements][r53-cmk]). Route 53 manages the zone-signing key. Signing
without a DS record at the registry is safe: resolvers treat the zone as
unsigned until the DS exists. Signing first lets `dns-check.sh` verify the
signatures and the DS digest before anything changes at the registrar.

Two DNSSEC providers cannot sign one zone at the same time ([migration step
5][r53-migrate-ds]), so the chain of trust breaks for a while: the old DS comes
out before the name servers change, and the new DS goes in after every resolver
has left Cloudflare. A name server change while the old DS is still cached makes
validating resolvers return SERVFAIL ([Cloudflare DNSSEC][cf-dnssec]).

Two alarms in `us-east-1`, `aboutme-prod-dnssec-internal-failure` and
`aboutme-prod-dnssec-ksk-action`, mail the alerts topic when Route 53 reports
`DNSSECInternalFailure` or `DNSSECKeySigningKeysNeedingAction`, as AWS
recommends ([DNSSEC signing][r53-dnssec]). Route 53 publishes both every four
hours. Either one means the KMS key or its policy needs attention before
validating resolvers start failing the domain.

## The move

Measured on 2026-09-26: the `.vn` servers publish the DS and the delegation with
TTL 43200 (12 hours), and Cloudflare's in-zone NS record has TTL 86400 (24
hours).

Before the first apply, add the Google Workspace values to the ignored
`prod.tfvars` (the shape is in `prod.tfvars.example`) and back it up to the
private infrastructure repository; `tofu plan` fails without them. Copy them
from the records Cloudflare serves today: the verification CNAME's first label
and target, and the `google._domainkey` TXT value with its strings joined.

1. **Apply and compare.** After `tofu apply`, run the comparison with the
   operator profile and the `cf` CLI signed in to the Cloudflare account:

   ```sh
   AWS_PROFILE=aboutme deploy/aws/scripts/dns-check.sh
   ```

   It queries the four Route 53 name servers and a Cloudflare name server
   directly, and fails on any record that differs, any Cloudflare record missing
   from Route 53, an alias address that does not serve the distribution, an
   HTTPS alias without `h2` and `h3`, or a DS digest that does not match the
   served key. It prints the CloudFront edge a Vietnamese client subnet gets
   (`DNS_CHECK_SUBNET`, default VNPT's `14.160.0.0/24`); expect `HAN` or `SGN`.
   Continue only when it prints `all checks passed`.

2. **Remove the DS at the registrar.** Delete the DS record for key tag 2371.
   Check that all eight `.vn` servers stop serving it:

   ```sh
   for s in a b c d e f g h; do dig +norec +short DS aboutme.vn @$s.dns-servers.vn; done
   ```

   The output must be empty. Then wait at least 12 hours, the DS TTL; the
   Cloudflare documentation allows 24 to 48 hours for most TLDs. After the wait,
   `dig +dnssec aboutme.vn A @8.8.8.8` shows no `ad` flag.

3. **Change the name servers at the registrar** to the four Route 53 names:

   ```sh
   tofu -chdir=deploy/aws/prod output -json dns_name_servers
   ```

   Enter them without the trailing dot, replacing `zahir.ns.cloudflare.com` and
   `dayana.ns.cloudflare.com`. Check the delegation and the path a resolver
   follows:

   ```sh
   for s in a b c d e f g h; do dig +norec NS aboutme.vn @$s.dns-servers.vn +noall +authority; done
   dig +trace aboutme.vn A
   curl -sI https://aboutme.vn/ | grep -i x-amz-cf-pop
   ```

   Every `.vn` server must list the four `awsdns` names, and the trace must end
   at an `awsdns` server. Wait 24 hours, Cloudflare's NS TTL, so that no
   resolver still queries Cloudflare. Watch the site, sign-in mail, and the
   `x-amz-cf-pop` values meanwhile. Cloudflare must keep answering during this
   wait, since resolvers that cached its NS record still ask it; check that
   `dig +norec SOA aboutme.vn @zahir.ns.cloudflare.com` still answers.

4. **Add the new DS at the registrar.** Get the values:

   ```sh
   tofu -chdir=deploy/aws/prod output -json dnssec
   ```

   Enter key tag `key_tag`, algorithm `13` (ECDSAP256SHA256), digest type `2`
   (SHA-256), and digest `digest`. A registrar that asks for the DNSKEY instead
   takes flags `257`, protocol `3`, algorithm `13`, and `public_key`. Check:

   ```sh
   for s in a b c d e f g h; do dig +norec +noall +answer DS aboutme.vn @$s.dns-servers.vn; done
   dig +dnssec aboutme.vn A @8.8.8.8
   dig +dnssec aboutme.vn A @1.1.1.1
   delv @1.1.1.1 aboutme.vn A
   dig +trace +dnssec aboutme.vn A
   ```

   Every `.vn` server returns `<key_tag> 13 2 <digest>` (dig splits the digest
   with a space; compare it without the space), both public resolvers set the
   `ad` flag, and `delv` prints `; fully validated`. Validating resolvers see
   the new DS within 12 hours. Watch for two weeks, as AWS recommends ([chain of
   trust][r53-enable]).

This order follows the AWS migration steps for a signed domain ([migrating a
domain in use][r53-migrate]: remove the DS, wait for the TTL, change the name
servers, then enable DNSSEC and add the DS) and Cloudflare's ([Cloudflare
DNSSEC][cf-dnssec]: remove the DS, wait for its TTL, change name servers, wait
for the old NS TTL, then add the new DS). The only difference from AWS's order
is that signing is on from the start, which is safe while no DS exists.

## Rollback

- Before step 3: add the old DS back
  (`2371 13 2 006A39A4D5419DB32B9BA95D930E0101AA1D8ACD3945C9F0359AF8206CADFEAC`);
  Cloudflare still signs with that key.
- After step 3, before step 4: set the name servers back to
  `zahir.ns.cloudflare.com` and `dayana.ns.cloudflare.com`. Cloudflare marks a
  free zone whose name servers moved away as Moved and deletes it 7 days later
  ([zone status][cf-status]); after that, adding the site again may assign new
  name servers. Add the old DS again only 24 hours after the name servers are
  back.
- After step 4: remove the DS first and wait 12 hours before any other change.

## Cloudflare cleanup

After step 4 has passed its two-week watch:

1. Export the Cloudflare zone file from the dashboard and keep it outside this
   repository.
2. Remove the site from Cloudflare, or let the Moved status delete it. Keep
   Cloudflare's DNSSEC signing on until then; disabling it first gains nothing.
3. Delete any Cloudflare API token scoped to `aboutme.vn`, and drop the zone
   from the Cloudflare MCP connection.
4. Update the docs that still describe Cloudflare DNS: the production runbook's
   Access line and Cloudflare DNS section, the email runbook's DNS section, the
   CloudFront runbook's return path to Cloudflare, the
   [CloudFront edge design](../design/cloudfront-edge.md), which should then
   cite the ADR that supersedes ADR 0054's DNS decision. Set this runbook's
   status to in production.

[r53-price]: https://aws.amazon.com/route53/pricing/
[kms-price]: https://aws.amazon.com/kms/pricing/
[r53-cmk]:
  https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-configuring-dnssec-cmk-requirements.html
[r53-dnssec]:
  https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-configuring-dnssec.html
[r53-enable]:
  https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-configuring-dnssec-enable-signing.html
[r53-migrate]:
  https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/migrate-dns-domain-in-use.html
[r53-migrate-ds]:
  https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/migrate-dns-domain-in-use.html#migrate-remove-ds
[cf-dnssec]: https://developers.cloudflare.com/dns/dnssec/
[cf-status]:
  https://developers.cloudflare.com/dns/zone-setups/reference/domain-status/
