# Authentication email

Status: **in production**. The app sends through Bizfly Email Transaction over
SMTP as `Danny from aboutme.vn <danny@aboutme.vn>`. Replies reach the Google
Workspace mailbox. This runbook covers mail without exposing credentials.

## Mail flow and DNS

The app sends verification, password-reset, and security-notification messages
through Bizfly. Google Workspace receives user, support, and security mail at
`danny@aboutme.vn`. Native development uses the loopback mail capture in the
[native development runbook](native-development.md).

Cloudflare is authoritative. The [DNS runbook](dns.md#records) owns the full
record inventory. The root SPF authorizes both Bizfly and Google Workspace.
Bizfly uses `dkim._domainkey` for its domain check and signs with
`etrans._domainkey`; both records hold the same key. Google Workspace uses
`google._domainkey`. The root MX is `smtp.google.com`, and DMARC uses
`p=quarantine` with no report address (owner decisions, 2026-10-15).

## Feedback

Bizfly delivery logs show the delivery result and SMTP reply. Bizfly provides no
webhook or suppression API, per the account checks in the
[host guide](../../deploy/vn/README.md#provider-questions). A successful SMTP
acceptance does not prove delivery to the recipient. Check Bizfly's logs when a
recipient reports missing mail.

The `aboutme-email` CloudFormation stack was deleted on 2026-10-11, including
its SES identity, configuration set, feedback queue, and alarms. SES feedback
and CloudWatch alarms are not production mail controls.

## Application configuration

`AUTH_EMAIL_MODE=smtp` sends authentication mail through Bizfly Email
Transaction. Each message includes a bilingual footer naming Bizfly's mail
server and the Google Workspace mailbox for replies. The host loads non-secret
settings from `/etc/aboutme/smtp.env` and credentials from Podman secrets. Set
these names in the runtime environment:

```dotenv
AUTH_EMAIL_MODE=smtp
SES_FROM_ADDRESS=danny@aboutme.vn
SES_FROM_NAME=Danny from aboutme.vn
SMTP_HOST=smtp.bizflycloud.vn
SMTP_PORT=465
SMTP_TLS=implicit
SMTP_USERNAME=danny@aboutme.vn
SMTP_PASSWORD=
```

Bizfly also accepts STARTTLS on port 587 (`SMTP_PORT=587`, `SMTP_TLS=starttls`).
The username is the sender address. `smtp-api.bizfly.vn` belongs to a different
Bizfly product and rejects these credentials.

- `SMTP_HOST` is a DNS name; the server certificate must be valid for it under
  the system roots.
- `SMTP_PORT` is any port from 1 to 65535 and has no default. `SMTP_TLS` is
  `implicit` or `starttls`, set explicitly; the sender never infers it from the
  port.
- `SMTP_USERNAME` and `SMTP_PASSWORD` are 1 to 256 bytes with no control
  characters. The loader preserves every byte, including leading and trailing
  spaces. The password comes from the host secret store, never from a tracked
  file.
- `SES_CONFIGURATION_SET` and the capture fields must be absent; `AWS_REGION` is
  ignored. The `SMTP_*` names must be absent in SES and capture mode.

A startup failure names the variable, never its value.

Each message opens one connection. The sender verifies the certificate, then
authenticates with `PLAIN` only after TLS is up; a STARTTLS server that does not
offer STARTTLS gets no credentials. A 2xx reply after `DATA` marks the job sent,
a 5xx reply marks it failed, and a 4xx reply, a timeout, a certificate failure,
or a transport error leaves it for retry.

A failed send logs `authmail: smtp send failed` with the closed `outcome` and,
when the server replied, its three-digit `code`. The log never carries the
recipient, subject, body, username, password, reply text, or server banner.
`code=535` points at the SMTP credentials; a failure with no code is a
connection, timeout, or certificate problem.

## Verification

Read the Cloudflare records with the checks in the [DNS runbook](dns.md#checks).
Use a test account to request verification or password reset. Confirm that the
message arrives, the sender is `Danny from aboutme.vn <danny@aboutme.vn>`, and
the link completes the expected flow. Reply to the message and confirm receipt
in Google Workspace. Check Bizfly delivery logs for failed delivery. Keep
recipients, links, and mail bodies out of committed evidence.
