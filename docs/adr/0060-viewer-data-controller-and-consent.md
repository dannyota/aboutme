# 0060: aboutme controls viewer data; detail only with consent

Status: Proposed (2026-09-26). The owner settled the feature set; the
product-visible choices in the
[viewer analytics design](../design/viewer-analytics/README.md#owner-approval)
wait for approval.

## Context

Resume owners want to know who reads their published resumes. The people who
read them are not aboutme users: they have no account, accepted no terms, and
often open the link from Zalo, LinkedIn, or email. Recorded views of a person on
an online service are sensitive personal data under Decree 356/2025/NĐ-CP
Article 4(1)(l), and collection needs consent first (Law 91/2025/QH15 Article
11(1)).

Two role models fit a hosted resume service:

| Model                                                            | Result                                                                                                                                                                                             |
| ---------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Owner is controller, aboutme is processor                        | aboutme would run a personal data processing service (Decree Article 21(1), (3)), which only an enterprise may run, under a Ministry of Public Security certificate (Decree Articles 22(1), 24(1)) |
| aboutme is controller and processor, owner receives with consent | aboutme fixes purposes, fields, and retention; it provides data to the owner with the viewer's consent (Law Article 15(2)(b))                                                                      |

## Decision

1. aboutme is the controller and processor (Law Article 2(9)) of all viewer
   data. Owners choose one of three fixed modes per resume, `count`, `ask`, or
   `sign_in`, and cannot change what is collected, why, or for how long.
2. Anonymous counts are kept for every published resume with no cookie and no
   stored personal data.
3. Any detail about a viewer is recorded only after that viewer's consent, given
   in a popup (`ask`) or on a sign-in gate (`sign_in`), with equal choices, a
   sensitive-data statement, and a stored consent record (Decree Article 6).
   Declining never limits reading the resume in `ask` mode.
4. View events are keyed per resume, so rows cannot be joined across owners
   without the viewer's cookie or sign-in.
5. Retention is 90 days for view data and 180 days for consent records.
6. Owners accept short terms before `ask` or `sign_in`; agents never read viewer
   data.

## Consequences

- The DPIA and the cross-border transfer assessment must cover viewer data.
- Viewers get self-service access, withdrawal, and deletion at
  `/privacy/viewer`, but only through their cookie or sign-in.
- Owners cannot add fields such as a custom question or a tracking pixel; any
  new field is a new decision under this record.
- The privacy notice drops "no analytics or tracking code" and describes both
  counting and viewer tracking.
