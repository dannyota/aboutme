# CloudFront verification

This page is part of the
[undeployed CloudFront edge reference](cloudfront-edge.md).

## Security parity

| Control              | Cloudflare proxy                       | CloudFront                                      |
| -------------------- | -------------------------------------- | ----------------------------------------------- |
| Viewer TLS           | Edge certificate, TLS 1.2+             | ACM certificate, `TLSv1.2_2021`                 |
| Origin reachability  | Security group: Cloudflare ranges      | Security group: CloudFront prefix list, 8443    |
| Origin authenticates | Origin pull mTLS, our CA               | Origin mTLS, our CA                             |
| Edge verifies origin | Full (strict), Origin CA               | Publicly trusted certificate, full chain        |
| Client address       | `CF-Connecting-IP` from trusted ranges | `CloudFront-Viewer-Address` on its own listener |
| Network DDoS         | Cloudflare                             | Shield Standard                                 |
| Application DDoS     | Free L7 mitigation, managed ruleset    | WAF rate rule, two managed groups (approval)    |
| HSTS, `nosniff`      | Cloudflare settings                    | Caddy                                           |
| Edge cache           | `/_nuxt/*` only                        | `/_nuxt/*` and the deployment document          |
| Decrypts traffic     | Cloudflare                             | AWS, already the hosting processor              |

Without the WAF, application-layer protection is weaker than Cloudflare's.

## Migration

Each step ships alone; the site stays on Cloudflare until step 5.

1. **Origin certificate.** Request the exportable certificate, add the two
   validation records at Cloudflare, export to SSM, and deploy. Cloudflare keeps
   working in Full (strict).
2. **Caddy edge listeners.** An `EDGES` list (`cloudflare`, `cloudfront`, or
   both) enables the 443 and 8443 listeners, with the client address rule, HSTS,
   `nosniff`, and `Server` removal. Tests cover forged headers per listener,
   IPv4 and unbracketed IPv6 values, duplicates, and a missing header. ADR
   0026's Caddy edge selection later adds `vcdn` to this list.
3. **Build the edge.** `tls.sh` stores the client CA parameter and imports the
   client certificate; OpenTofu adds the viewer certificate, the security group,
   the policies, the distribution, and the web ACL; task definitions get
   `EDGES=cloudflare,cloudfront` and the trust pool.
4. **Test before cutover** with `aboutme.vn` and `www.aboutme.vn` pinned to the
   distribution's edge addresses (`curl --resolve`, Chromium
   `--host-resolver-rules`), running the [test plan](#test-plan). The real
   hostname keeps `PUBLIC_ORIGIN`, cookies, CSRF origin checks, and passkeys
   valid; a temporary hostname would fail them. **Owner approval:** pinned
   hostnames instead of a temporary one.
5. **Cutover.** Raise the release fence and deploy the privacy notice. At
   Cloudflare, replace the apex `A` with a DNS-only CNAME to the distribution
   and point `www` at it, TTL 60. Proxied answers carry TTL 300
   ([TTL][cfl-ttl]), so old resolvers reach the 443 listener for about five
   minutes. Watch `x-amz-cf-pop`, 5xx rates, and the site-down alarm.
6. **Soak seven days** with Cloudflare's SSL, origin pull, and cache settings
   intact.
7. **Remove Cloudflare from the path:** `EDGES=cloudfront`; delete the 443
   rules, `CLOUDFLARE_RANGES`, the range check, and the old pull CA parameter;
   remove the zone origin pull certificate; revoke the Origin CA certificate;
   update the living docs. **Owner approval:** add CAA `0 issue "amazon.com"`
   then, not earlier, since it blocks Cloudflare's edge certificate on rollback.

**Rollback** before step 7: restore the proxied apex `A` to the Elastic IP and
the proxied `www` CNAME; Cloudflare serves within its TTL and 443 never stopped.
After step 7, revert that step, apply, and deploy first.

## Test plan

Before cutover with pinned hostnames and again after; evidence under
`.dev/prod-checks/cloudfront/`.

1. **Origin closed:** `https://<EIP>:443` and `:8443` time out from the laptop;
   a throwaway distribution without the client certificate gets 502.
2. **Client address:** sign-in records the tester's real IPv4 and IPv6 address
   in the sessions list; forged `X-Forwarded-For`, `X-Real-IP`,
   `CF-Connecting-IP`, `Forwarded`, and `CloudFront-Viewer-Address` change
   nothing; login rate limits trip for one address, not a second.
3. **SSE:** an editor stream stays open 10 minutes without reconnecting, and an
   edit in a second tab arrives.
4. **MCP:** discovery, authorization, token, `tools/list`, an edit, and
   `upload_photo` with 2 MiB, on the owner test account.
5. **Uploads:** a 2 MiB editor photo succeeds; an oversized one gets Go's error,
   not an edge error.
6. **Cache:** a hashed `/_nuxt/` file hits on the second request; `?v=` values
   cache apart; `/`, `/healthz`, `/api/*`, a public page, its PDF, and its photo
   always miss; unpublish gives 404 at once; `If-None-Match` gets 304.
7. **Headers:** CSPs, `Clear-Site-Data`, HSTS, `nosniff`, and `__Host-` cookies
   present; `Server: Caddy`, `Via: 1.1 Caddy`, and `CF-Ray` absent from served
   responses (Caddy's own empty 502 and 504 keep its defaults); `www` redirects.
8. **Deploy:** a one-second poll of `/` and `/healthz` through a deploy and a
   `--rollback` sees only 200 or the marked 503, never 502, 504, or a timeout.
9. **Latency:** the origin latency poll from Vietnam and one distant network
   shows a nearby `x-amz-cf-pop` and first byte under one second.

## Facts to verify

1. CloudFront sends the forwarded `Host` as the SNI name; the docs require the
   certificate to match it but do not name the SNI value ([origin
   TLS][cf-origin-tls]). If not, set Caddy's `default_sni` on 8443.
2. The IPv6 `CloudFront-Viewer-Address` format; a viewer value is replaced.
3. Viewer `If-None-Match` reaches the origin with cookies forwarded.
4. Error caching TTL 0 keeps the maintenance 503 off `/_nuxt/*` after a deploy.
5. Origin mTLS with the imported ECDSA certificate on pay-as-you-go through
   OpenTofu (the locked AWS provider 6.64.0 has `origin_mtls_config`).
6. Changing the instance's security groups applies in place.
7. The exported leaf and chain fit the 4 KB parameter limit.
8. From Vietnam, the apex through `1.1.1.1` and `8.8.8.8` reaches `HAN` or `SGN`
   after the name server change.
9. Imported ACM certificates carry no charge.

[cf-origin-tls]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-https-cloudfront-to-custom-origin.html
[cfl-ttl]:
  https://developers.cloudflare.com/dns/manage-dns-records/reference/ttl/
