# Production DNS

Cloudflare is authoritative for `aboutme.vn` since 2026-10-10 20:56 UTC, on the
Free plan. Every record is DNS-only, with proxying off. The OpenTofu root
`deploy/vn/dns/` owns the records under
[ADR 0026](../adr/0026-vietnam-hosted-production.md). Change records through a
reviewed configuration or local input change and a reviewed plan. Do not edit
managed records in the dashboard.

## Records

| Name                           | Type  | Value or purpose                                                  |
| ------------------------------ | ----- | ----------------------------------------------------------------- |
| `aboutme.vn`, `www.aboutme.vn` | A     | Host floating IP, TTL 300                                         |
| `aboutme.vn`                   | CAA   | `0 issue "letsencrypt.org"`                                       |
| `aboutme.vn`                   | MX    | `smtp.google.com`, Google Workspace mailbox                       |
| `aboutme.vn`                   | TXT   | `v=spf1 include:_spf.bizflycloud.vn include:_spf.google.com ~all` |
| `_dmarc.aboutme.vn`            | TXT   | `v=DMARC1; p=quarantine`                                          |
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

## OpenTofu inputs and credentials

The DNS root uses OpenTofu 1.12.6 and Cloudflare provider 5.27.0. It takes the
`aboutme.vn` zone ID as `zone_id`, avoiding a zone-list lookup. The owner checks
that ID against the zone overview before import. Host plans in `deploy/vn/prod/`
do not need a Cloudflare token.

Create an API token with **Zone DNS Edit** and **Zone Read**, scoped to
**aboutme.vn only**. Keep the token in the owner's private credential store or
an ignored, mode-0600 environment file with an encrypted backup. Load it only in
the process that needs it as `CLOUDFLARE_API_TOKEN`. The provider has no token
variable. Never put the token in `dns.tfvars`, backend configuration, command
history, logs, CI, or tracked files. Disable shell tracing before loading it.

Copy `deploy/vn/dns/backend.hcl.example` to ignored `backend.hcl`, and
`dns.tfvars.example` to ignored `dns.tfvars`. Fill the host IPv4 address, zone
ID, three exact DKIM TXT contents, and Google verification name and target. Use
the full verification name under `aboutme.vn` and the target without a trailing
dot. Preserve the API's TXT quoting in DKIM values. Keep verification values and
DKIM material out of the repository. Fill `record_ttls` and `mx_priority` from
the existing records; the A records have fixed TTL 300. Cloudflare TTL `1` means
automatic.

State uses the same HCM04 vStorage S3 backend and lock-file pattern as the host
root, with a separate key, `dns/terraform.tfstate`. Set the backend bucket and
credential-file path in `backend.hcl`. Load `TF_VAR_state_passphrase` from the
private credential store through the environment; PBKDF2 and AES-GCM encrypt
state and saved plans. Keep its encrypted backup with the backend credentials.

Locally exclude `deploy/vn/dns/dns.tfvars`, `backend.hcl`, `.terraform/`,
`*.tfstate`, `*.tfstate.*`, and `*.tfplan` in `.git/info/exclude`. Keep API
inventory files outside the checkout with mode 0600. Agents must not read these
inputs or inventories. CI initializes without the backend and tests mocked plans
without credentials.

## Adopt existing records once

1. Load the scoped token and state passphrase without printing them. Set
   `CLOUDFLARE_ZONE_ID` to the checked `aboutme.vn` zone ID. From the owner's
   workstation, list record metadata into a private file. The command reads the
   token from the environment, sends it through standard input to curl, and
   emits no TXT contents:

   ```sh
   umask 077
   mkdir -p "$HOME/.config/aboutme/vn"
   printf 'header = "Authorization: Bearer %s"\n' "$CLOUDFLARE_API_TOKEN" |
     curl --fail --silent --show-error --config - \
       "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/dns_records?per_page=100" |
     jq -e 'if .success and .result_info.total_pages <= 1 then
       [.result[] | {id, name, type, ttl, priority}]
       else error("DNS inventory failed or needs pagination") end' \
       > "$HOME/.config/aboutme/vn/dns-records.json"
   ```

2. Match the ten managed records to the [inventory](#records). Fill all ten
   `record_ids` entries in `dns.tfvars`: `apex`, `www`, `caa`, `mx`, `spf`,
   `dmarc`, `dkim`, `etrans`, `google_dkim`, and `google_verification`. The
   `caa` ID must be the Let's Encrypt entry, not an automatic Cloudflare entry.
   Copy exact DKIM contents privately from Cloudflare. Fill existing non-A TTLs
   and MX priority from the metadata. Do not import automatic CAA entries.
3. Initialize and save an encrypted import plan:

   ```sh
   tofu -chdir=deploy/vn/dns init -backend-config=backend.hcl -input=false
   tofu -chdir=deploy/vn/dns plan -var-file=dns.tfvars -out=adopt.tfplan
   ```

4. Accept the plan only if it shows **10 imports, 0 creates, 0 updates, and 0
   destroys**. Check every resource address, zone ID, record ID, name, value,
   TTL, and proxy flag against Cloudflare. If any record would change, stop and
   reconcile the input or configuration through review. Do not apply a plan that
   changes records while importing. TXT quoting and provider normalization also
   need this live comparison; mocked tests cannot prove a no-change import.
5. The primary applies only the reviewed saved plan:

   ```sh
   tofu -chdir=deploy/vn/dns apply adopt.tfplan
   tofu -chdir=deploy/vn/dns plan -var-file=dns.tfvars
   ```

6. After all ten records are in state and the next plan shows no changes, delete
   `deploy/vn/dns/imports.tf` in a reviewed change and remove the local
   `record_ids` entries (or set the map to `{}`). Keep the blocks until that
   confirmation. Deleting import blocks does not delete records from state.
   Delete the saved plan after use.

## Change records

Review the configuration change and update ignored inputs privately when needed.
Load the same scoped token, backend credentials, and state passphrase, then run:

```sh
tofu -chdir=deploy/vn/dns init -backend-config=backend.hcl -input=false
tofu -chdir=deploy/vn/dns plan -var-file=dns.tfvars -out=change.tfplan
```

The primary reviews the exact plan before running:

```sh
tofu -chdir=deploy/vn/dns apply change.tfplan
tofu -chdir=deploy/vn/dns plan -var-file=dns.tfvars
```

Stop on unexpected creates, updates, or destroys. The apex and `www` A records
and MX record have `prevent_destroy`; do not remove this protection to bypass an
unexpected plan. Confirm the DNS checks after applying and delete the saved
plan. Protect local plan output because verification values can appear in it.

## DNSSEC

DNSSEC stays outside OpenTofu; the root has no DNSSEC resource. Cloudflare signs
the zone with algorithm 13 (ECDSAP256SHA256). The owner copies Cloudflare's DS
record from the dashboard to the `.vn` registrar. Signing the zone and
publishing the DS record are separate steps. Do not assume the registrar step is
complete from Cloudflare signing alone.

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
