-- +goose Up
-- Phase PA (D1/D3): password credentials, pending registrations, reset tokens,
-- and the encrypted email outbox. The migration is additive; existing accounts
-- keep working with no credential.
--
-- Preflight: the shared canonical-email parser (Phase PA D1) becomes
-- authoritative for account creation and lookup in T03. Every existing account
-- email must therefore already satisfy the canonical form before any password
-- table exists; otherwise a legacy row would be stranded behind a parser it can
-- no longer satisfy. The check below is a pure SELECT that aborts the whole
-- migration transaction (goose runs it in one transaction) before any table is
-- created or row changed. The full addr-spec grammar is verified by the Go
-- parser's own live-data test in T03; this preflight is the cheap, reliable
-- subset: lowercase, pure ASCII, bounded length, no whitespace/controls,
-- exactly one "@", and a non-empty dot-separated domain.
-- +goose StatementBegin
DO $$
BEGIN
  IF (SELECT count(*) FROM users
      WHERE email::text <> lower(email::text)
         OR octet_length(email::text) < 5
         OR octet_length(email::text) > 254
         OR octet_length(email::text) <> char_length(email::text)
         OR email::text ~ '[[:space:]]'
         OR email::text ~ '[[:cntrl:]]'
         OR length(email::text) - length(replace(email::text, '@', '')) <> 1
         OR split_part(email::text, '@', 1) = ''
         OR split_part(email::text, '@', 2) = ''
         OR position('.' IN split_part(email::text, '@', 2)) = 0) > 0 THEN
    RAISE EXCEPTION 'password auth migration preflight: noncanonical account email';
  END IF;
END $$;
-- +goose StatementEnd

-- create "password_credentials" table
CREATE TABLE "public"."password_credentials" (
  "user_id" uuid NOT NULL,
  "encoded_hash" bytea NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "changed_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("user_id"),
  CONSTRAINT "password_credentials_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "password_credentials_encoded_hash_length_check" CHECK (octet_length(encoded_hash) BETWEEN 1 AND 192),
  CONSTRAINT "password_credentials_changed_after_created_check" CHECK (changed_at >= created_at)
);

-- create "password_registrations" table
CREATE TABLE "public"."password_registrations" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "email" public.citext NOT NULL,
  "name" text NOT NULL,
  "encoded_hash" bytea NOT NULL,
  "token_digest" bytea NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "expires_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "password_registrations_email_key" UNIQUE ("email"),
  CONSTRAINT "password_registrations_token_digest_key" UNIQUE ("token_digest"),
  CONSTRAINT "password_registrations_name_length_check" CHECK (octet_length(name) BETWEEN 1 AND 400),
  CONSTRAINT "password_registrations_encoded_hash_length_check" CHECK (octet_length(encoded_hash) BETWEEN 1 AND 192),
  CONSTRAINT "password_registrations_token_digest_length_check" CHECK (octet_length(token_digest) = 32),
  CONSTRAINT "password_registrations_expiry_check" CHECK (expires_at = created_at + interval '24 hours')
);
-- create index "password_registrations_expires_at_idx" to table: "password_registrations"
CREATE INDEX "password_registrations_expires_at_idx" ON "public"."password_registrations" ("expires_at");

-- create "password_reset_tokens" table
CREATE TABLE "public"."password_reset_tokens" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "user_id" uuid NOT NULL,
  "token_digest" bytea NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "expires_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "password_reset_tokens_user_id_key" UNIQUE ("user_id"),
  CONSTRAINT "password_reset_tokens_token_digest_key" UNIQUE ("token_digest"),
  CONSTRAINT "password_reset_tokens_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "password_reset_tokens_token_digest_length_check" CHECK (octet_length(token_digest) = 32),
  CONSTRAINT "password_reset_tokens_expiry_check" CHECK (expires_at = created_at + interval '30 minutes')
);
-- create index "password_reset_tokens_expires_at_idx" to table: "password_reset_tokens"
CREATE INDEX "password_reset_tokens_expires_at_idx" ON "public"."password_reset_tokens" ("expires_at");

-- create "auth_email_jobs" table
CREATE TABLE "public"."auth_email_jobs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "kind" text NOT NULL,
  "state" text NOT NULL,
  "registration_id" uuid NULL,
  "reset_token_id" uuid NULL,
  "user_id" uuid NULL,
  "token_digest" bytea NULL,
  "key_id" text NULL,
  "nonce" bytea NULL,
  "ciphertext" bytea NULL,
  "attempts" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "expires_at" timestamptz NOT NULL,
  "next_attempt_at" timestamptz NULL,
  "lease_owner" text NULL,
  "lease_expires_at" timestamptz NULL,
  "sent_at" timestamptz NULL,
  "terminal_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "auth_email_jobs_registration_id_fkey" FOREIGN KEY ("registration_id") REFERENCES "public"."password_registrations" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "auth_email_jobs_reset_token_id_fkey" FOREIGN KEY ("reset_token_id") REFERENCES "public"."password_reset_tokens" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "auth_email_jobs_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "auth_email_jobs_kind_check" CHECK (kind = ANY (ARRAY['verify'::text, 'reset'::text, 'password_changed'::text])),
  CONSTRAINT "auth_email_jobs_state_check" CHECK (state = ANY (ARRAY['pending'::text, 'leased'::text, 'sent'::text, 'terminal'::text])),
  CONSTRAINT "auth_email_jobs_scope_check" CHECK (
    (kind = 'verify' AND registration_id IS NOT NULL AND reset_token_id IS NULL AND user_id IS NULL)
    OR (kind = 'reset' AND reset_token_id IS NOT NULL AND registration_id IS NULL AND user_id IS NULL)
    OR (kind = 'password_changed' AND user_id IS NOT NULL AND registration_id IS NULL AND reset_token_id IS NULL)
  ),
  CONSTRAINT "auth_email_jobs_token_digest_required_check" CHECK (
    (kind = ANY (ARRAY['verify'::text, 'reset'::text])) = (token_digest IS NOT NULL)
  ),
  CONSTRAINT "auth_email_jobs_token_digest_length_check" CHECK (token_digest IS NULL OR octet_length(token_digest) = 32),
  CONSTRAINT "auth_email_jobs_key_id_check" CHECK (key_id IS NULL OR (octet_length(key_id) BETWEEN 1 AND 64 AND key_id !~ '[^ -~]')),
  CONSTRAINT "auth_email_jobs_nonce_check" CHECK (nonce IS NULL OR octet_length(nonce) = 12),
  CONSTRAINT "auth_email_jobs_ciphertext_check" CHECK (ciphertext IS NULL OR octet_length(ciphertext) BETWEEN 1 AND 4112),
  CONSTRAINT "auth_email_jobs_attempts_check" CHECK (
    (state = 'pending' AND attempts BETWEEN 0 AND 7)
    OR (state = ANY (ARRAY['leased'::text, 'sent'::text]) AND attempts BETWEEN 1 AND 8)
    OR (state = 'terminal' AND attempts BETWEEN 0 AND 8)
  ),
  CONSTRAINT "auth_email_jobs_expiry_check" CHECK (expires_at <= created_at + interval '24 hours'),
  CONSTRAINT "auth_email_jobs_state_matrix_check" CHECK (
    (state = 'pending' AND key_id IS NOT NULL AND nonce IS NOT NULL AND ciphertext IS NOT NULL
      AND next_attempt_at IS NOT NULL AND lease_owner IS NULL AND lease_expires_at IS NULL
      AND sent_at IS NULL AND terminal_at IS NULL)
    OR (state = 'leased' AND key_id IS NOT NULL AND nonce IS NOT NULL AND ciphertext IS NOT NULL
      AND next_attempt_at IS NULL AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL
      AND sent_at IS NULL AND terminal_at IS NULL)
    OR (state = 'sent' AND key_id IS NULL AND nonce IS NULL AND ciphertext IS NULL
      AND next_attempt_at IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL
      AND sent_at IS NOT NULL AND terminal_at IS NULL)
    OR (state = 'terminal' AND key_id IS NULL AND nonce IS NULL AND ciphertext IS NULL
      AND next_attempt_at IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL
      AND sent_at IS NULL AND terminal_at IS NOT NULL)
  )
);
-- create index "auth_email_jobs_claim_idx" to table: "auth_email_jobs"
CREATE INDEX "auth_email_jobs_claim_idx" ON "public"."auth_email_jobs" ("next_attempt_at", "created_at", "id") WHERE (state = 'pending');
-- create index "auth_email_jobs_outcome_idx" to table: "auth_email_jobs"
CREATE INDEX "auth_email_jobs_outcome_idx" ON "public"."auth_email_jobs" ("sent_at", "terminal_at") WHERE (state = ANY (ARRAY['sent'::text, 'terminal'::text]));

-- +goose Down
-- reverse: create index "auth_email_jobs_outcome_idx" to table: "auth_email_jobs"
DROP INDEX "public"."auth_email_jobs_outcome_idx";
-- reverse: create index "auth_email_jobs_claim_idx" to table: "auth_email_jobs"
DROP INDEX "public"."auth_email_jobs_claim_idx";
-- reverse: create "auth_email_jobs" table
DROP TABLE "public"."auth_email_jobs";
-- reverse: create index "password_reset_tokens_expires_at_idx" to table: "password_reset_tokens"
DROP INDEX "public"."password_reset_tokens_expires_at_idx";
-- reverse: create "password_reset_tokens" table
DROP TABLE "public"."password_reset_tokens";
-- reverse: create index "password_registrations_expires_at_idx" to table: "password_registrations"
DROP INDEX "public"."password_registrations_expires_at_idx";
-- reverse: create "password_registrations" table
DROP TABLE "public"."password_registrations";
-- reverse: create "password_credentials" table
DROP TABLE "public"."password_credentials";
