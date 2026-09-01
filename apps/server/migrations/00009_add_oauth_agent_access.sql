-- +goose Up
-- Phase PM (M1-M3, M5): first-party OAuth 2.1 agent-access storage — clients,
-- authorization codes, grants, and tokens. The migration is purely additive;
-- an account that never authorizes an agent is unaffected.
--
-- Storage is digest-only. Authorization codes and access/refresh tokens exist
-- in PostgreSQL solely as 32-byte SHA-256 digests, and PKCE code verifiers are
-- never stored at all — only the S256 challenge the client published. Every
-- bound below (closed kinds and scopes, digest length, code TTL, token and
-- family lifetimes, single live grant per (user, client), redirect-URI count)
-- is enforced by the database, not by a Go pass that happens to agree with it.
--
-- No preflight is needed: the migration creates four new tables and reads no
-- existing row.

-- create "oauth_clients" table
-- The primary key IS the public client_id (M1: a UUID string, no secret), so
-- there is no second identifier that could drift from it. redirect_uris is a
-- jsonb array bounded to 1-5 printable-ASCII strings of 1-512 bytes each; the
-- element regex is split across three repetition groups because PostgreSQL
-- caps a single {m,n} count at 255.
CREATE TABLE "public"."oauth_clients" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "client_name" text NOT NULL,
  "redirect_uris" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "last_used_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "oauth_clients_client_name_length_check" CHECK (
    char_length(client_name) BETWEEN 1 AND 64
    AND octet_length(client_name) BETWEEN 1 AND 256
  ),
  CONSTRAINT "oauth_clients_client_name_control_check" CHECK (client_name !~ '[[:cntrl:]]'),
  CONSTRAINT "oauth_clients_redirect_uris_check" CHECK (
    jsonb_typeof(redirect_uris) = 'array'
    AND jsonb_array_length(redirect_uris) BETWEEN 1 AND 5
    AND NOT jsonb_path_exists(redirect_uris, '$[*] ? (@.type() != "string")')
    AND NOT jsonb_path_exists(redirect_uris, '$[*] ? (!(@ like_regex "^[!-~]{1,255}[!-~]{0,255}[!-~]{0,2}$"))')
  ),
  CONSTRAINT "oauth_clients_last_used_order_check" CHECK (last_used_at >= created_at)
);
-- create index "oauth_clients_created_at_idx" to table: "oauth_clients"
-- Drives the bounded idle-client sweep oldest-first, so repeated sweeps make
-- monotonic progress instead of rescanning the same rows.
CREATE INDEX "oauth_clients_created_at_idx" ON "public"."oauth_clients" ("created_at", "id");

-- create "oauth_authorization_codes" table
-- A code binds client, user, scopes, the S256 challenge, and the exact
-- redirect URI presented at authorize time; the token request must repeat that
-- URI. expires_at is pinned to exactly 60 seconds after creation (M2), and a
-- consumed row records the token family it issued so a replay revokes exactly
-- those tokens.
CREATE TABLE "public"."oauth_authorization_codes" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "code_digest" bytea NOT NULL,
  "client_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "scopes" text NOT NULL,
  "code_challenge" text NOT NULL,
  "redirect_uri" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "expires_at" timestamptz NOT NULL,
  "consumed_at" timestamptz NULL,
  "issued_family_id" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "oauth_authorization_codes_code_digest_key" UNIQUE ("code_digest"),
  CONSTRAINT "oauth_authorization_codes_client_id_fkey" FOREIGN KEY ("client_id") REFERENCES "public"."oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "oauth_authorization_codes_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "oauth_authorization_codes_code_digest_length_check" CHECK (octet_length(code_digest) = 32),
  CONSTRAINT "oauth_authorization_codes_scopes_check" CHECK (
    scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text])
  ),
  CONSTRAINT "oauth_authorization_codes_code_challenge_check" CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
  CONSTRAINT "oauth_authorization_codes_redirect_uri_check" CHECK (
    redirect_uri ~ '^[!-~]{1,255}[!-~]{0,255}[!-~]{0,2}$'
  ),
  CONSTRAINT "oauth_authorization_codes_expiry_check" CHECK (expires_at = created_at + interval '60 seconds'),
  CONSTRAINT "oauth_authorization_codes_consumed_family_check" CHECK (
    (consumed_at IS NULL) = (issued_family_id IS NULL)
  ),
  CONSTRAINT "oauth_authorization_codes_consumed_order_check" CHECK (
    consumed_at IS NULL OR consumed_at >= created_at
  )
);
-- create index "oauth_authorization_codes_expires_at_idx" to table: "oauth_authorization_codes"
CREATE INDEX "oauth_authorization_codes_expires_at_idx" ON "public"."oauth_authorization_codes" ("expires_at", "id");
-- create index "oauth_authorization_codes_client_id_idx" to table: "oauth_authorization_codes"
CREATE INDEX "oauth_authorization_codes_client_id_idx" ON "public"."oauth_authorization_codes" ("client_id");
-- create index "oauth_authorization_codes_user_id_idx" to table: "oauth_authorization_codes"
CREATE INDEX "oauth_authorization_codes_user_id_idx" ON "public"."oauth_authorization_codes" ("user_id");

-- create "oauth_grants" table
-- The partial unique index — not application code — is what makes a second
-- live grant for one (user, client) impossible: two concurrent consent
-- approvals contend on it and exactly one survives. A revoked grant keeps its
-- row for audit and leaves the live slot free.
CREATE TABLE "public"."oauth_grants" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "user_id" uuid NOT NULL,
  "client_id" uuid NOT NULL,
  "scopes" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "revoked_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "oauth_grants_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "oauth_grants_client_id_fkey" FOREIGN KEY ("client_id") REFERENCES "public"."oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "oauth_grants_scopes_check" CHECK (
    scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text])
  ),
  CONSTRAINT "oauth_grants_revoked_order_check" CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
-- create index "oauth_grants_live_user_client_key" to table: "oauth_grants"
CREATE UNIQUE INDEX "oauth_grants_live_user_client_key" ON "public"."oauth_grants" ("user_id", "client_id") WHERE (revoked_at IS NULL);
-- create index "oauth_grants_client_live_idx" to table: "oauth_grants"
CREATE INDEX "oauth_grants_client_live_idx" ON "public"."oauth_grants" ("client_id") WHERE (revoked_at IS NULL);

-- create "oauth_tokens" table
-- Access and refresh tokens share one table keyed by a 32-byte digest. A
-- refresh family is identified by family_id and dies at family_expires_at; no
-- token may outlive its family, and an access token may not outlive one hour.
-- rotated_from is the exact one-successor lineage link, mirroring sessions:
-- the partial unique index below means one predecessor can never mint two
-- successors, so a rotation race has exactly one winner.
CREATE TABLE "public"."oauth_tokens" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "token_digest" bytea NOT NULL,
  "kind" text NOT NULL,
  "family_id" uuid NOT NULL,
  "rotated_from" uuid NULL,
  "client_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "grant_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "expires_at" timestamptz NOT NULL,
  "family_expires_at" timestamptz NOT NULL,
  "revoked_at" timestamptz NULL,
  "superseded_at" timestamptz NULL,
  "last_used_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "oauth_tokens_token_digest_key" UNIQUE ("token_digest"),
  CONSTRAINT "oauth_tokens_rotated_from_fkey" FOREIGN KEY ("rotated_from") REFERENCES "public"."oauth_tokens" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "oauth_tokens_client_id_fkey" FOREIGN KEY ("client_id") REFERENCES "public"."oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "oauth_tokens_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "oauth_tokens_grant_id_fkey" FOREIGN KEY ("grant_id") REFERENCES "public"."oauth_grants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "oauth_tokens_token_digest_length_check" CHECK (octet_length(token_digest) = 32),
  CONSTRAINT "oauth_tokens_kind_check" CHECK (kind = ANY (ARRAY['access'::text, 'refresh'::text])),
  CONSTRAINT "oauth_tokens_rotated_from_self_check" CHECK (rotated_from IS NULL OR rotated_from <> id),
  CONSTRAINT "oauth_tokens_expiry_order_check" CHECK (expires_at > created_at AND expires_at <= family_expires_at),
  CONSTRAINT "oauth_tokens_family_lifetime_check" CHECK (family_expires_at <= created_at + interval '30 days'),
  CONSTRAINT "oauth_tokens_access_lifetime_check" CHECK (
    kind <> 'access' OR expires_at <= created_at + interval '1 hour'
  ),
  CONSTRAINT "oauth_tokens_revoked_order_check" CHECK (revoked_at IS NULL OR revoked_at >= created_at),
  CONSTRAINT "oauth_tokens_superseded_order_check" CHECK (superseded_at IS NULL OR superseded_at >= created_at),
  CONSTRAINT "oauth_tokens_last_used_order_check" CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);
-- create index "oauth_tokens_rotated_from_key" to table: "oauth_tokens"
CREATE UNIQUE INDEX "oauth_tokens_rotated_from_key" ON "public"."oauth_tokens" ("rotated_from") WHERE (rotated_from IS NOT NULL);
-- create index "oauth_tokens_family_id_idx" to table: "oauth_tokens"
CREATE INDEX "oauth_tokens_family_id_idx" ON "public"."oauth_tokens" ("family_id");
-- create index "oauth_tokens_grant_id_idx" to table: "oauth_tokens"
CREATE INDEX "oauth_tokens_grant_id_idx" ON "public"."oauth_tokens" ("grant_id");
-- create index "oauth_tokens_user_id_idx" to table: "oauth_tokens"
CREATE INDEX "oauth_tokens_user_id_idx" ON "public"."oauth_tokens" ("user_id");
-- create index "oauth_tokens_client_live_idx" to table: "oauth_tokens"
CREATE INDEX "oauth_tokens_client_live_idx" ON "public"."oauth_tokens" ("client_id") WHERE (revoked_at IS NULL AND superseded_at IS NULL);
-- create index "oauth_tokens_cleanup_idx" to table: "oauth_tokens"
CREATE INDEX "oauth_tokens_cleanup_idx" ON "public"."oauth_tokens" ("expires_at", "id");

-- +goose Down
-- reverse: create index "oauth_tokens_cleanup_idx" to table: "oauth_tokens"
DROP INDEX "public"."oauth_tokens_cleanup_idx";
-- reverse: create index "oauth_tokens_client_live_idx" to table: "oauth_tokens"
DROP INDEX "public"."oauth_tokens_client_live_idx";
-- reverse: create index "oauth_tokens_user_id_idx" to table: "oauth_tokens"
DROP INDEX "public"."oauth_tokens_user_id_idx";
-- reverse: create index "oauth_tokens_grant_id_idx" to table: "oauth_tokens"
DROP INDEX "public"."oauth_tokens_grant_id_idx";
-- reverse: create index "oauth_tokens_family_id_idx" to table: "oauth_tokens"
DROP INDEX "public"."oauth_tokens_family_id_idx";
-- reverse: create index "oauth_tokens_rotated_from_key" to table: "oauth_tokens"
DROP INDEX "public"."oauth_tokens_rotated_from_key";
-- reverse: create "oauth_tokens" table
DROP TABLE "public"."oauth_tokens";
-- reverse: create index "oauth_grants_client_live_idx" to table: "oauth_grants"
DROP INDEX "public"."oauth_grants_client_live_idx";
-- reverse: create index "oauth_grants_live_user_client_key" to table: "oauth_grants"
DROP INDEX "public"."oauth_grants_live_user_client_key";
-- reverse: create "oauth_grants" table
DROP TABLE "public"."oauth_grants";
-- reverse: create index "oauth_authorization_codes_user_id_idx" to table: "oauth_authorization_codes"
DROP INDEX "public"."oauth_authorization_codes_user_id_idx";
-- reverse: create index "oauth_authorization_codes_client_id_idx" to table: "oauth_authorization_codes"
DROP INDEX "public"."oauth_authorization_codes_client_id_idx";
-- reverse: create index "oauth_authorization_codes_expires_at_idx" to table: "oauth_authorization_codes"
DROP INDEX "public"."oauth_authorization_codes_expires_at_idx";
-- reverse: create "oauth_authorization_codes" table
DROP TABLE "public"."oauth_authorization_codes";
-- reverse: create index "oauth_clients_created_at_idx" to table: "oauth_clients"
DROP INDEX "public"."oauth_clients_created_at_idx";
-- reverse: create "oauth_clients" table
DROP TABLE "public"."oauth_clients";
