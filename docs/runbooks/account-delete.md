# Account delete

Status: **operational**. This runbook covers how the operator deletes the
account that owns a reported showcase slug. It extends
[the production runbook](production.md), which owns the deploy role, the
operation lock, and recovery. The [showcase design](../design/showcase.md) owns
the rules; [ADR 0029](../adr/0029-community-showcase.md) owns the decision, and
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

Run from the repository root, with the owner's login, at the tag the app runs:

```sh
export AWS_PROFILE=aboutme
tag=<live tag>  # DEPLOY_RELEASE_TAG in the aboutme-prod-app task definition
bash deploy/aws/scripts/deploy.sh --account-delete "$tag" show <slug>
bash deploy/aws/scripts/deploy.sh --account-delete "$tag" confirm <slug> <user-id>
```

The script checks its arguments before any AWS call. The slug follows the public
slug grammar and the user ID is a lowercase UUID. It then verifies the tag-bound
provenance of the tag's server image, takes the operation lock, and requires the
running app to be that exact tag and healthy. It registers a `jobs` revision at
that image and runs one task with the command
`account-delete <subcommand> <arguments>`. It never changes a service, a
schedule, a snapshot, or the release fence, and it releases the lock when the
task stops.

The deploy and operator roles cannot read CloudWatch Logs, so the script reads
the task log with the owner's own login (`/aboutme/prod`, stream
`jobs/jobs/<task id>`) and prints it without the end line. The output holds the
user ID, slugs, action, and outcome only, never an email, name, or headline.
Control characters and non-ASCII bytes are dropped before printing.

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

## What is kept

The task log keeps one audit line per run with the action, outcome, user ID,
slugs, and time, for 180 days with the server logs. A slug tombstone is not
linked to the account, and the deletion removes the rest.

## Failures

- A usage or argument error exits 2 before any AWS call.
- Exit 4 means no resume holds the slug: it is unknown, tombstoned, or already
  deleted. Nothing was deleted.
- Exit 5 means the slug is not owned by that user ID. Nothing was deleted. Run
  `show <slug>` again and use the user ID it prints.
- Any other non-zero task exit prints the task log, then exits 1 with the exit
  code. The deletion is one transaction, so a failed run deleted nothing.
- Every run that reaches the database ends its log with the line
  `account-delete: end`. The script reads the log from the start, follows pages
  until the token repeats, and retries up to five times. It never prints that
  line. If the line never appears, the script prints what it read, says the
  result is incomplete, and exits 1, even when the task exited 0. It prints
  `done` only after a complete log with exit 0.
- When the result is unknown, run `show <slug>`. Exit 4 with `not found` means
  the account is deleted. A printed `account` line means it still exists, so run
  `confirm` again.
- If RunTask fails or its response is lost, or ECS cannot confirm the task
  stopped, the script leaves the operation lock closed, because the task may
  still run. The message names the `started-by` value
  (`acctdel-<operation id prefix>`). List tasks with it, wait for `STOPPED`,
  then clear the lock as [the production runbook](production.md) describes.
