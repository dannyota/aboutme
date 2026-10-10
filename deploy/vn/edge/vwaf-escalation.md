# vWAF escalation checklist

Use this when an HTTP flood is larger than the host can absorb and CrowdSec,
Coraza, and Go's rate limits are not enough. The decision and its limits are in
[Vietnam production](../../../docs/design/vietnam-production.md#edge). Do it in
the GreenNode root portal; no script covers it.

Everything marked **Unconfirmed** has not been checked on our account. Check it
before you rely on it, and write the result in the production runbook.

## Before you start

- [ ] Confirm the flood is HTTP and reaches Caddy, with the Caddy and CrowdSec
      logs. A network-level flood is a different problem (design Q1).
- [ ] Find vWAF's price and capacity. **Unconfirmed:** both.
- [ ] Find the source addresses vWAF connects from, and how it passes the client
      address to the origin (header name and format). **Unconfirmed:** both.
- [ ] Check how Caddy renews its certificate once DNS points at vWAF. Caddy gets
      certificates by ACME HTTP-01 on port 80, so the challenge must pass
      through vWAF to the host. **Unconfirmed:** whether vWAF forwards
      `/.well-known/acme-challenge/`. Check the days left on the current
      certificate; the escalation must not outlast it without a plan.
- [ ] Lower the TTL of the apex A record and the `www` record at Route 53 to 60
      seconds. Wait at least the old TTL before you change any record.

## Keep the client address right

Caddy takes the client address from the socket and strips every forwarding
header. Behind vWAF the socket address is vWAF's, so Go's rate limits and
CrowdSec would see one client. Before the cutover:

- [ ] Change the Caddy configuration to trust vWAF's header only from vWAF's
      source addresses, and send the real address to Go as the single
      `X-Real-IP`.
- [ ] Test it with a request that carries a forged header from another address.
      The forged value must not reach Go.
- [ ] Open the security group for ports 80 and 443 to vWAF's source addresses
      only after the cutover works. Until then the host stays open to all, as
      today.

## Create the application

- [ ] In the root portal, create a vWAF application for `aboutme.vn` and
      `www.aboutme.vn`, with the floating IP as the upstream over HTTPS.
- [ ] Note the vWAF code and its address. The portal gives the CNAME target
      `<code>.waf.greennode.vn`.
- [ ] Start in detection or log-only mode if the portal offers it.
      **Unconfirmed:** the available modes.
- [ ] Test before any DNS change: send requests to vWAF's address with the
      `Host` header set to `aboutme.vn`, and check `/readyz` and a public
      resume page.

## Cut over

Route 53 aliases point only at AWS targets, so use plain records.

- [ ] Pause the vMonitor check `aboutme-prod-readyz` so the change does not
      page. Resume it at the end.
- [ ] Change `www` to a CNAME to `<code>.waf.greennode.vn`.
- [ ] Change the apex to an A record with vWAF's address. A CNAME is not allowed
      at the apex. **Unconfirmed:** that vWAF's address is fixed; check the
      portal for changes before each use.
- [ ] Watch the Caddy access log: requests now come from vWAF's addresses, with
      the real client address in the trusted header.

## Verify

- [ ] `dig +short aboutme.vn` and `dig +short www.aboutme.vn` return vWAF's
      address and the CNAME chain.
- [ ] `curl -sI https://aboutme.vn/readyz` returns 200 with a valid
      certificate, and so does `https://www.aboutme.vn/`.
- [ ] Sign in, open a resume, and download a PDF from a browser.
- [ ] A CrowdSec-banned test address still gets 403, using its real address.
- [ ] The attack traffic shows in the vWAF portal and falls in the Caddy log.
- [ ] Resume the vMonitor check and wait for it to read Up.

## Privacy

vWAF decrypts traffic in Vietnam, so GreenNode sees every request and response
in clear text. The privacy notice already names GreenNode as a processor for
hosting; it covers this. Confirm that vWAF logs stay in Vietnam and how long
they are kept. **Unconfirmed:** both. Update the notice if either answer
differs from the hosting terms.

## Roll back

- [ ] Point the apex A record back to the floating IP and `www` back to the
      same A record, or a CNAME to the apex.
- [ ] Keep the TTL at 60 seconds until the traffic is back on the host, then
      raise it to the normal value.
- [ ] Remove the trusted vWAF header from Caddy only if the escalation is over.
- [ ] If the flood continues, close the security group to everything except
      vWAF's source addresses before you leave vWAF on.
- [ ] Record the dates, the flood, and the result in the production runbook.
