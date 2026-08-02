-- +goose Up
-- create "users" table
CREATE TABLE "public"."users" ("id" uuid NOT NULL DEFAULT uuidv7(), "email" public.citext NOT NULL, "name" text NOT NULL, "avatar_key" text NULL, "created_at" timestamptz NOT NULL DEFAULT now(), "updated_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "users_email_key" UNIQUE ("email"));
-- create "identities" table
CREATE TABLE "public"."identities" ("id" uuid NOT NULL DEFAULT uuidv7(), "user_id" uuid NOT NULL, "provider" text NOT NULL, "provider_user_id" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "identities_provider_subject_key" UNIQUE ("provider", "provider_user_id"), CONSTRAINT "identities_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT "identities_provider_check" CHECK (provider = ANY (ARRAY['google'::text, 'github'::text, 'linkedin'::text])));
-- create index "identities_user_id_idx" to table: "identities"
CREATE INDEX "identities_user_id_idx" ON "public"."identities" ("user_id");
-- create "oauth_transactions" table
CREATE TABLE "public"."oauth_transactions" ("id" uuid NOT NULL DEFAULT uuidv7(), "handle_hash" bytea NOT NULL, "provider" text NOT NULL, "purpose" text NOT NULL, "linking_user_id" uuid NULL, "state" text NOT NULL, "pkce_verifier" text NOT NULL, "nonce" text NULL, "redirect_uri" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT now(), "expires_at" timestamptz NOT NULL, "consumed_at" timestamptz NULL, PRIMARY KEY ("id"), CONSTRAINT "oauth_transactions_handle_hash_key" UNIQUE ("handle_hash"), CONSTRAINT "oauth_transactions_linking_user_id_fkey" FOREIGN KEY ("linking_user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT "oauth_transactions_link_needs_user" CHECK (((purpose = ANY (ARRAY['link'::text, 'reauth'::text])) AND (linking_user_id IS NOT NULL)) OR ((purpose = 'login'::text) AND (linking_user_id IS NULL))), CONSTRAINT "oauth_transactions_provider_check" CHECK (provider = ANY (ARRAY['google'::text, 'github'::text, 'linkedin'::text])), CONSTRAINT "oauth_transactions_purpose_check" CHECK (purpose = ANY (ARRAY['login'::text, 'link'::text, 'reauth'::text])));
-- create index "oauth_transactions_expires_at_idx" to table: "oauth_transactions"
CREATE INDEX "oauth_transactions_expires_at_idx" ON "public"."oauth_transactions" ("expires_at");
-- create "sessions" table
CREATE TABLE "public"."sessions" ("id" uuid NOT NULL DEFAULT uuidv7(), "user_id" uuid NOT NULL, "token_hash" bytea NOT NULL, "csrf_secret" bytea NOT NULL, "created_at" timestamptz NOT NULL DEFAULT now(), "last_seen_at" timestamptz NOT NULL DEFAULT now(), "reauthenticated_at" timestamptz NOT NULL DEFAULT now(), "absolute_expires_at" timestamptz NOT NULL, "rotation_grace_until" timestamptz NULL, "revoked_at" timestamptz NULL, "ua" text NULL, "ip" inet NULL, PRIMARY KEY ("id"), CONSTRAINT "sessions_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- create index "sessions_token_hash_key" to table: "sessions"
CREATE UNIQUE INDEX "sessions_token_hash_key" ON "public"."sessions" ("token_hash");
-- create index "sessions_user_id_active_idx" to table: "sessions"
CREATE INDEX "sessions_user_id_active_idx" ON "public"."sessions" ("user_id") WHERE (revoked_at IS NULL);

-- +goose Down
-- reverse: create index "sessions_user_id_active_idx" to table: "sessions"
DROP INDEX "public"."sessions_user_id_active_idx";
-- reverse: create index "sessions_token_hash_key" to table: "sessions"
DROP INDEX "public"."sessions_token_hash_key";
-- reverse: create "sessions" table
DROP TABLE "public"."sessions";
-- reverse: create index "oauth_transactions_expires_at_idx" to table: "oauth_transactions"
DROP INDEX "public"."oauth_transactions_expires_at_idx";
-- reverse: create "oauth_transactions" table
DROP TABLE "public"."oauth_transactions";
-- reverse: create index "identities_user_id_idx" to table: "identities"
DROP INDEX "public"."identities_user_id_idx";
-- reverse: create "identities" table
DROP TABLE "public"."identities";
-- reverse: create "users" table
DROP TABLE "public"."users";
