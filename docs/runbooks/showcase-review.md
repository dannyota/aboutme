# Showcase review

Status: **operational**. This runbook covers how the operator reviews community
showcase listings. It extends [the production runbook](production.md), which
owns the deploy role, the operation lock, and recovery. The
[showcase design](../design/showcase.md) owns the rules;
[ADR 0029](../adr/0029-community-showcase.md) owns the decision, and
[ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md) is why the
review has no screen in the app.

A resume appears in the showcase only while its current review key equals the
approved one. A new opt-in, and any change to the name, headline, photo, link,
or language, sends it back to `pending`.

## Run a review command

Run from the repository root, with the owner's login, at the tag the app runs:

```sh
export AWS_PROFILE=aboutme
tag=<live tag>  # DEPLOY_RELEASE_TAG in the aboutme-prod-app task definition
bash deploy/aws/scripts/deploy.sh --showcase-review "$tag" pending
bash deploy/aws/scripts/deploy.sh --showcase-review "$tag" show <slug>
bash deploy/aws/scripts/deploy.sh --showcase-review "$tag" approve <slug> <key>
bash deploy/aws/scripts/deploy.sh --showcase-review "$tag" decline <slug> <key>
```

The script checks its arguments before any AWS call. The slug follows the public
slug grammar and the key is 16 lowercase hex digits. It then verifies the
tag-bound provenance of the tag's server image, takes the operation lock, and
requires the running app to be that exact tag and healthy. It registers a `jobs`
revision at that image and runs one task with the command
`showcase-review <subcommand> <arguments>`. It never changes a service, a
schedule, a snapshot, or the release fence, and it releases the lock when the
task stops.

The deploy and operator roles cannot read CloudWatch Logs, so the script reads
the task log with the owner's own login (`/aboutme/prod`, stream
`jobs/jobs/<task id>`) and prints it. The output holds slugs, review keys, card
versions, dates, and states only, never a name or headline. Control characters
and non-ASCII bytes are dropped before printing.

## Approve or decline

1. Run `pending`. Each line has a slug, a review key, a card version, and the
   request date.
2. Open `https://aboutme.vn/<slug>` and the card at
   `https://aboutme.vn/api/v1/public/resumes/<slug>/og/<version>.png`, using the
   printed version.
3. Check the listing against every item below.
4. Run `approve <slug> <key>` or `decline <slug> <key>` with the printed key.
5. Run `show <slug>` and confirm the state is `listed` or `declined`.

Approve only a listing that meets all of these:

- A real person's resume in the listed language, with the name matching the
  person.
- No impersonation.
- No illegal or offensive text or image.
- No one else's personal data.
- No sensitive data (ID numbers, health, religion, political views).
- No advertising or spam links.
- A photo that is a portrait, or no photo.
- No sign that the resume belongs to someone under 16 (the Terms age).

A key that changed since `pending` printed makes `approve` and `decline` change
nothing and say so. Run `pending` again and review the new card. `decline`
removes a listed resume from the showcase at once and keeps it out until the
review key changes and a new review approves it. To take down content that
breaks the Terms, run `decline` with the current key from `show`.

## Failures

- A usage or argument error exits 2 before any AWS call.
- A non-zero task exit prints the task log, then exits 1 with the exit code.
- If the task exits 0 but its log is empty or unreadable, the script exits 1 and
  says the result is unknown. Run `show <slug>` before repeating an `approve` or
  `decline`.
- If RunTask fails or its response is lost, or ECS cannot confirm the task
  stopped, the script leaves the operation lock closed, because the task may
  still run. The message names the `started-by` value
  (`showcase-<operation id prefix>`). List tasks with it, wait for `STOPPED`,
  then clear the lock as [the production runbook](production.md) describes.
- The commands are idempotent for one key, so a repeat after a known failure is
  safe once `show` confirms the state.
