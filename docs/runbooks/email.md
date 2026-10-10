# Authentication email

Status: **in production**. Google Workspace and AWS SES are configured for
`aboutme.vn`, and the SES account has production access. This runbook records
the current setup and the checks an operator can repeat without exposing
credentials or AWS account identifiers.

## Mail flow

`danny@aboutme.vn` is the user-facing mailbox and sender. Google Workspace
receives normal user, support, and security mail. Authentication mail is sent
transactionally through AWS SES in `ap-southeast-1` with:

- From address: `danny@aboutme.vn`, shown as `Danny from aboutme.vn`
- Configuration set: `aboutme-auth`
- CloudFormation stack: `aboutme-email`
- SES domain identity: `aboutme.vn`
- Easy DKIM: 2048-bit, with three generated CNAME records
- Custom MAIL FROM: `bounce.aboutme.vn`

CloudFormation outputs expose the three SES DKIM CNAMEs. Keep their generated
tokens out of this public runbook.

The application uses SES only for transactional authentication messages:
verification, password reset, and security notifications. Native development
uses the loopback mail capture described in the
[native development runbook](native-development.md).

## Cloudflare DNS

Cloudflare is DNS-only for these records. The root records are:

- MX priority 1: `smtp.google.com`
- SPF: `v=spf1 include:_spf.google.com ~all`
- Google DKIM selector: `google._domainkey`
- Retained Google domain-verification CNAME
- DMARC at `_dmarc`: `v=DMARC1; p=none; rua=mailto:danny@aboutme.vn`

SES uses its own DKIM selectors and authenticates its custom MAIL FROM subdomain
separately. Therefore root SPF remains Google-only. The MAIL FROM records are:

- `bounce.aboutme.vn` MX: `feedback-smtp.ap-southeast-1.amazonses.com`
- `bounce.aboutme.vn` SPF: `v=spf1 include:amazonses.com ~all`

Do not replace the Google root MX or root SPF with SES records.

OpenTofu's `dns` module recreates these records in the Route 53 zone prepared in
the [DNS runbook](dns.md). Until the name servers move, change a record at
Cloudflare and in that module together.

## Reputation and feedback

SES account-level suppression is enabled for both `BOUNCE` and `COMPLAINT`. The
SES event destination publishes `SEND`, `DELIVERY`, `BOUNCE`, `COMPLAINT`,
`REJECT`, `RENDERING_FAILURE`, and `DELIVERY_DELAY` metrics. CloudWatch alarms
notify an SNS email subscription at `danny@aboutme.vn`.

Private bounce and complaint events also flow through SNS to the encrypted SQS
queue `aboutme-ses-feedback`, which retains messages for 24 hours. No queue
consumer exists yet; do not treat queue delivery as application processing.

## Application configuration

Set these names and values in the runtime environment:

```dotenv
AUTH_EMAIL_MODE=ses
AWS_REGION=ap-southeast-1
SES_FROM_ADDRESS=danny@aboutme.vn
SES_FROM_NAME=Danny from aboutme.vn
SES_CONFIGURATION_SET=aboutme-auth
```

AWS credentials use the runtime credential chain. Never put credentials in
`.env.example`, source, images, commands, logs, or this runbook.

## Account limits

The SES account has production access: 50,000 messages per 24 hours and 14
messages per second (checked 2026-09-24 with `aws sesv2 get-account`). It can
send to any address. Use the mailbox simulator for smoke tests.

The app task role grants only `ses:SendEmail`, which the SES v2 sender uses,
limited to the configured from address. Add another sending action only with a
documented caller and an affected policy test.

The existing `aboutme-email` CloudFormation stack keeps owning its SES, SNS, SQS
and CloudWatch resources. Production OpenTofu only grants the app task role
permission to send from the verified identity; it creates no overlapping
resource. Moving the stack into OpenTofu later needs its own plan: manage the
stack as one unit, or transfer retained resources out of CloudFormation before
importing them, with a no-change plan and rollback steps. Never delete the stack
as a shortcut.

## Send failures

A failed send logs `authmail: ses send failed` with the closed `outcome` and,
when SES returns one, its error `code`. The log never carries the recipient,
body, request ID, or SES error message. `code=MessageRejected` usually means SES
refused the message content or the from identity; `code=AccessDeniedException`
points at the app task role's `ses:SendEmail` policy.

## SMTP mode

`AUTH_EMAIL_MODE=smtp` sends authentication mail through an SMTP relay instead
of SES, as the
[Vietnam production design](../design/vietnam-production.md#dns-and-mail) sets
for Bizfly Email Transaction. It uses the same From address, display name,
templates, and sealed payloads as SES mode. Set these names in the runtime
environment:

```dotenv
AUTH_EMAIL_MODE=smtp
SES_FROM_ADDRESS=danny@aboutme.vn
SES_FROM_NAME=Danny from aboutme.vn
SMTP_HOST=
SMTP_PORT=
SMTP_TLS=
SMTP_USERNAME=
SMTP_PASSWORD=
```

- `SMTP_HOST` is a DNS name; the server certificate must be valid for it under
  the system roots.
- `SMTP_TLS=implicit` requires `SMTP_PORT=465`; `SMTP_TLS=starttls` requires
  `SMTP_PORT=587`. Any other pair fails at startup.
- `SMTP_USERNAME` and `SMTP_PASSWORD` are 1 to 256 bytes with no control
  characters. The password comes from the host secret store, never from a
  tracked file.
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

Run from a workstation with the AWS CLI configured for the intended account. The
commands below read state; they do not create or modify resources.

```sh
dig +short MX aboutme.vn
dig +short TXT aboutme.vn
dig +short TXT google._domainkey.aboutme.vn
dig +short CNAME <ses-dkim-token-1>._domainkey.aboutme.vn
dig +short CNAME <ses-dkim-token-2>._domainkey.aboutme.vn
dig +short CNAME <ses-dkim-token-3>._domainkey.aboutme.vn
dig +short CNAME <google-verification-name>.aboutme.vn
dig +short TXT _dmarc.aboutme.vn
dig +short MX bounce.aboutme.vn
dig +short TXT bounce.aboutme.vn

aws sesv2 get-email-identity \
  --email-identity aboutme.vn \
  --region ap-southeast-1
aws sesv2 get-account --region ap-southeast-1
aws sns list-subscriptions --region ap-southeast-1
aws cloudformation describe-stacks \
  --stack-name aboutme-email \
  --query 'Stacks[0].Outputs' \
  --output table
```

Replace the angle-bracket DNS names with the values exposed by the
CloudFormation outputs or the Cloudflare zone. Do not paste the generated SES
tokens into committed documentation.

Send a non-production smoke message to the SES mailbox simulator. The simulator
address is not a real recipient:

```sh
aws sesv2 send-email \
  --from-email-address danny@aboutme.vn \
  --destination 'ToAddresses=success@simulator.amazonses.com' \
  --configuration-set-name aboutme-auth \
  --content 'Simple={Subject={Data=aboutme SES smoke test},Body={Text={Data=mailbox simulator check}}}' \
  --region ap-southeast-1
```

Confirm the command returns a message ID, then inspect the configuration-set
metrics and CloudWatch alarm state. Send smoke tests to the mailbox simulator,
not to a real mailbox.
