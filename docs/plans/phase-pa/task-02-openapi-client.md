# Task 02 — Define password OpenAPI and generated client contracts

**Acceptance:** AC-AUTH-009, AC-AUTH-011, AC-AUTH-012, AC-AUTH-013, AC-AUTH-015.

**Depends on:** T00 authority and T01 stable storage vocabulary.

**Owned paths:** T02 paths in `file-structure.md`. This is a serialized
OpenAPI/generated-client owner window.

## Contract

Add operations without `/api/v1` in the OpenAPI path keys:

| Method and path                | `operationId`              |
| ------------------------------ | -------------------------- |
| `POST /auth/password/register` | `postAuthPasswordRegister` |
| `POST /auth/password/verify`   | `postAuthPasswordVerify`   |
| `POST /auth/password/login`    | `postAuthPasswordLogin`    |
| `POST /auth/password/forgot`   | `postAuthPasswordForgot`   |
| `POST /auth/password/reset`    | `postAuthPasswordReset`    |
| `POST /auth/password/reauth`   | `postAuthPasswordReauth`   |
| `PUT /me/password`             | `putMePassword`            |

Every request schema is closed and non-null. Email is a string of at most 254
ASCII bytes; password is a string whose raw JSON-decoded value is capped again
by the server at 1,024 UTF-8 bytes; token is exactly 43 base64url characters;
registration name has the D1 limits. OpenAPI documents the 4,096-byte transport
cap and exact response table from D9. `MeUser.hasPassword` is required Boolean.

`PasswordAccepted` is the sole 202 body. Policy details use a closed issue enum.
No schema contains password confirmation, provider email, token state, raw
token, hash, mail job, key ID, or dependency detail.

The response status/code matrix is closed:

| Operation  | Success | Other admitted status/code pairs                                                                                                                                                                                                   |
| ---------- | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Register   | `202`   | `400 request_invalid`; `403 csrf_rejected`; `413 body_too_large`; `415 media_type_unsupported`; `422 password_invalid`; `429 rate_limited`; `503 authentication_unavailable`                                                       |
| Verify     | `204`   | `400 request_invalid`; `400 credential_token_invalid`; `403 csrf_rejected`; `413 body_too_large`; `415 media_type_unsupported`; `429 rate_limited`; `503 authentication_unavailable`                                               |
| Login      | `204`   | `400 request_invalid`; `401 authentication_failed`; `403 csrf_rejected`; `413 body_too_large`; `415 media_type_unsupported`; `429 rate_limited`; `503 authentication_unavailable`                                                  |
| Forgot     | `202`   | `400 request_invalid`; `403 csrf_rejected`; `413 body_too_large`; `415 media_type_unsupported`; `429 rate_limited`; `503 authentication_unavailable`                                                                               |
| Reset      | `204`   | `400 request_invalid`; `400 credential_token_invalid`; `403 csrf_rejected`; `413 body_too_large`; `415 media_type_unsupported`; `422 password_invalid`; `429 rate_limited`; `503 authentication_unavailable`                       |
| Reauth     | `204`   | `400 request_invalid`; `401 authentication_required`; `401 reauth_failed`; `403 csrf_rejected`; `413 body_too_large`; `415 media_type_unsupported`; `429 rate_limited`; `503 authentication_unavailable`                           |
| Set/change | `204`   | `400 request_invalid`; `401 authentication_required`; `403 csrf_rejected`; `403 reauth_required`; `413 body_too_large`; `415 media_type_unsupported`; `422 password_invalid`; `429 rate_limited`; `503 authentication_unavailable` |

Existing router `404`, `405`, and outer `429` responses remain governed by the
shared route chain and appear on every applicable operation.

## TDD cycle

- [ ] Add failing OpenAPI tests for all methods, operation IDs, request
      required/closed fields, statuses, examples, headers, `hasPassword`, and
      forbidden sensitive property names.
- [ ] Add failing generated-client parity tests that instantiate every request
      type and require `PUT` support for `/me/password`.
- [ ] Run:

  ```sh
  make api-check
  ```

  Confirm RED is only the missing password contract/client.

- [ ] Add components and operations with existing envelope/error conventions.
      Reference shared errors only when status, code, and body match exactly;
      add password-specific components otherwise.
- [ ] Regenerate TypeScript with `apps/web/scripts/openapi-gen.sh`; never edit
      `openapi.ts` by hand.
- [ ] Assert the generator output has no `any`, nullable `hasPassword`, optional
      required field, or password-confirmation type.
- [ ] Rerun:

  ```sh
  make api-check web-typecheck
  ```

## Adversarial checklist

- Missing, null, wrong type, unknown property, duplicate-property runtime rule,
  and limit+1 are represented by the contract/test plan.
- Register/forgot expose the same success component and no alternate response.
- Login, reauth, token, password-policy, rate, media type, body cap, and
  dependency outcomes are closed and route-specific.
- Success responses never return session/token/password/email state; the session
  exists only in the secure cookie.

## Handoff

Report operation IDs, generated TypeScript names, exact statuses/components,
RED/GREEN commands, and consumer symbols for T10/T11. Suggested commit:
`feat(api): define password authentication contract`.
