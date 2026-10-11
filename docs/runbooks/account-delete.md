# Account delete

Status: **open host procedure**. This runbook covers how the operator deletes
the account that owns a reported showcase slug. It extends
[the production runbook](production.md), which owns the host operation lock and
recovery. The [showcase design](../design/showcase.md) owns the rules;
[ADR 0029](../adr/0029-community-showcase.md) owns the decision, and
[ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md) is why the
command has no screen in the app.

## When to use it

Use it after a showcase Report, or other abuse, that shows a serious breach of
the Terms or a legal duty to act. The deletion is permanent and sends no notice
email, so a minor or doubtful report does not justify it. Nothing in the app
holds the reporter's identity, and the deletion ends the account's showcase
listing, its resumes, its media, and its sessions together.

The deletion follows the same path as a self-delete from Settings. Each slug of
the account becomes a tombstone that blocks reuse for 180 days.

## Run it

The AWS ECS deletion procedure no longer applies to production. The Vietnam
scripts, including `host/job-run.sh` and the host bin scripts, provide no
reviewed abuse account-deletion path. Do not run the AWS deployer or improvise a
host command. The steps below define the checks a reviewed host procedure must
keep; operator deletion remains blocked until that procedure exists.

## Steps

1. Read the report email. Note the slug in its subject.
2. Open `https://aboutme.vn/<slug>` and confirm the content breaches the Terms
   or the law.
3. Run `show <slug>`. It prints one line,
   `account user=<uuid> slugs=<slug1,slug2,...>`, and changes nothing.
4. Check that the slug in the report is in that list. If it is not, stop.
5. Run `confirm <slug> <user-id>` with the printed user ID. The server checks
   the owner again inside the deletion transaction and deletes only on a match.
6. Open `https://aboutme.vn/<slug>`. It must return the public 404 page.
7. Run `show <slug>` again. It must exit 4 with `not found`, because the slug is
   now a tombstone. Do the same for the other slugs of the account.

A response the server admitted before the commit finishes sending, and a live
stream ends. The server drops its cached sitemap and `llms.txt` on the deletion
notification ([ADR 0010](../adr/0010-public-artifact-revocation.md), as
amended), so the next request sees the account gone.

## Audit and failures

The reviewed host procedure must record action, outcome, user ID, slugs, and
time without email, name, headline, or secret values. Host audit retention and
access remain part of the open procedure; AWS task logs and CloudTrail are not
live audit stores.

The account-delete command returns exit 4 when the slug is absent and exit 5
when the owner does not match. Neither outcome deletes an account. An unknown
commit result requires a fresh `show <slug>` check before retrying `confirm`. If
the host cannot prove the command stopped, retain the operation lock until
inspection proves quiescence. Follow the
[host lock-clear safeguards](production.md#passkey-release-fence).
