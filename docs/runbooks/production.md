# Production

Status: **infrastructure built; not yet serving** (2026-09-17). This runbook
covers the single-host production environment at `https://aboutme.vn`. The
[single-host design](../design/single-host-production.md) explains why it is
shaped this way. Deploy, rollback, restore and rotation sections are added when
their scripts exist.

## Access

- AWS: `aws login` into the owner's account, region `ap-southeast-1`.
- OpenTofu: `deploy/aws/prod` with the ignored `backend.hcl` and `prod.tfvars`.
  Every `tofu plan` and `tofu apply` takes `-var-file=prod.tfvars`, because
  state encryption reads the key ARN before a saved plan loads.
- Cloudflare: the owner's authenticated Cloudflare MCP connection. There is no
  Cloudflare API token and no Cloudflare provider in OpenTofu.

```sh
tofu -chdir=deploy/aws/prod plan -var-file=prod.tfvars
```

A clean environment prints `No changes.`

## Cloudflare settings

These live only in Cloudflare. Change them through the MCP connection or the
dashboard, and update this table in the same change.

| Setting                    | Value                                                                                                 |
| -------------------------- | ----------------------------------------------------------------------------------------------------- |
| `aboutme.vn`               | `A` to the host Elastic IP (`tofu output host_public_ip`), proxied                                    |
| `www.aboutme.vn`           | `CNAME` to `aboutme.vn`, proxied; the origin redirects it to the apex                                 |
| SSL/TLS mode               | Full (strict)                                                                                         |
| Always Use HTTPS           | On                                                                                                    |
| Minimum TLS version        | 1.2; TLS 1.3 on                                                                                       |
| HSTS                       | On, max-age 31536000, no subdomains, no preload, nosniff                                              |
| Bot Fight Mode             | Off                                                                                                   |
| Cache rule                 | `not starts_with(http.request.uri.path, "/_nuxt/")` → bypass cache                                    |
| Origin CA certificate      | ECC, `aboutme.vn` and `www.aboutme.vn`, expires 2041-09-12; stored at `/aboutme/prod/tls/origin-cert` |
| Authenticated Origin Pulls | Zone-level certificate from `tls.sh`; enable only after the first healthy deploy                      |

Mail records (MX, TXT, DKIM and the SES `bounce` records) predate this setup and
stay unchanged.

Before a deploy, `deploy.sh` compares Cloudflare's published IPv4 ranges with
the ranges in the running task definition. If they differ, run `tofu apply` to
refresh the security group and the Caddy trust list first.

## Secrets

Values live in SSM Parameter Store under `/aboutme/prod/`. Never print them.

- `deploy/aws/scripts/secrets.sh` creates missing database passwords and keys
  and never overwrites one.
- `deploy/aws/scripts/tls.sh` creates the origin key and certificate request and
  the origin-pull client certificate. It leaves the origin-pull files in
  `$XDG_RUNTIME_DIR/aboutme-origin-pull` for the dashboard upload (SSL/TLS →
  Origin Server → Authenticated Origin Pulls); run `tls.sh --forget-pull`
  afterwards.
- The RDS master password is managed by RDS in Secrets Manager. Only the
  one-shot `db-admin` tasks can read it.

List names only:

```sh
aws ssm get-parameters-by-path --path /aboutme/prod --recursive --query 'Parameters[].Name'
```

## Expected state before the first deploy

- ECS services `aboutme-prod-app` and `aboutme-prod-web` run zero tasks.
- `https://aboutme.vn/` returns Cloudflare error 521.
- A request straight to the Elastic IP gets no response.
