# 0060: aboutme controls viewer counting and keeps no viewer data

Status: Accepted (2026-09-26), narrowed. The owner dropped consented viewer
tracking for good, so this record now covers only anonymous counts and the
transient viewer data they use. The earlier decisions about per-view detail, a
consent popup, viewer cookies, consent records, owner terms, and a viewer data
page are withdrawn.

## Context

Resume owners want to know whether their published resumes are read. The people
who read them are not aboutme users: they have no account, accepted no terms,
and often open the link from Zalo, LinkedIn, or email. Telling a real reader
from a bot needs the request's IP address and user agent for a moment, which is
personal data while it is processed (Law 91/2025/QH15 Article 2(1)). Recorded
views of a person online are sensitive personal data (Decree 356/2025/NĐ-CP
Article 4(1)(l)).

A service that processes personal data on behalf of others is a personal data
processing service, which only an enterprise may run, under a Ministry of Public
Security certificate (Decree Articles 21(1), (3), 22(1), 24(1)). aboutme is run
by an individual.

## Decision

1. aboutme is the controller and processor (Law Article 2(9)) of the transient
   viewer data counting uses, and fixes the fields, filters, and retention for
   every resume. Owners cannot change what is processed.
2. aboutme records nothing about any viewer. It stores only daily totals per
   resume, which are not personal data, and keeps the IP address, user agent,
   and the day's network key in memory only.
3. Counting is always on for every published resume and needs no consent; the
   privacy notice discloses it. Owners receive only totals.
4. Daily totals are kept 400 days. Agents never read them.

## Consequences

- No consent popup, viewer cookie, consent record, viewer rights page, or owner
  terms exist, and no release waits on a DPIA update for viewer data; the next
  regular update notes the transient processing.
- The privacy notice replaces "No analytics or tracking scripts." with a
  statement that views are counted as described, and never implies tracking.
- Sign in to view follows the same rule and keeps nothing about the viewer
  ([ADR 0062](0062-sign-in-to-view-without-an-account.md)).
- Any future feature that records a viewer needs a new decision.
