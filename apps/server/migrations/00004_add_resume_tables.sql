-- +goose Up
-- create "idempotency_records" table
CREATE TABLE "public"."idempotency_records" ("id" uuid NOT NULL DEFAULT uuidv7(), "user_id" uuid NOT NULL, "route" text NOT NULL, "idempotency_key" uuid NOT NULL, "request_hash" bytea NOT NULL, "response_status" integer NOT NULL, "response_body" jsonb NOT NULL, "created_at" timestamptz NOT NULL DEFAULT now(), "expires_at" timestamptz NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "idempotency_records_user_route_key_key" UNIQUE ("user_id", "route", "idempotency_key"), CONSTRAINT "idempotency_records_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- create index "idempotency_records_expires_at_idx" to table: "idempotency_records"
CREATE INDEX "idempotency_records_expires_at_idx" ON "public"."idempotency_records" ("expires_at");
-- create "resumes" table
CREATE TABLE "public"."resumes" ("id" uuid NOT NULL DEFAULT uuidv7(), "user_id" uuid NOT NULL, "title" text NOT NULL, "slug" text NULL, "live" boolean NOT NULL DEFAULT false, "download_enabled" boolean NOT NULL DEFAULT true, "seo_geo_enabled" boolean NOT NULL DEFAULT false, "schema_version" integer NOT NULL, "revision" bigint NOT NULL DEFAULT 1, "lng" text NULL, "personal_details" jsonb NOT NULL, "content" jsonb NOT NULL, "customization" jsonb NOT NULL, "created_at" timestamptz NOT NULL DEFAULT now(), "updated_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "resumes_slug_key" UNIQUE ("slug"), CONSTRAINT "resumes_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT "resumes_live_requires_slug_check" CHECK ((NOT live) OR (slug IS NOT NULL)), CONSTRAINT "resumes_lng_length_check" CHECK ((lng IS NULL) OR (char_length(lng) <= 35)), CONSTRAINT "resumes_revision_check" CHECK (revision >= 1), CONSTRAINT "resumes_schema_version_check" CHECK (schema_version >= 1), CONSTRAINT "resumes_seo_requires_live_check" CHECK ((NOT seo_geo_enabled) OR live), CONSTRAINT "resumes_slug_format_check" CHECK ((slug IS NULL) OR ((char_length(slug) >= 4) AND (char_length(slug) <= 30) AND (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text))), CONSTRAINT "resumes_title_length_check" CHECK (char_length(title) <= 160));
-- create index "resumes_user_id_idx" to table: "resumes"
CREATE INDEX "resumes_user_id_idx" ON "public"."resumes" ("user_id");
-- create "slug_tombstones" table
CREATE TABLE "public"."slug_tombstones" ("id" uuid NOT NULL DEFAULT uuidv7(), "slug" text NOT NULL, "released_by_user_id" uuid NULL, "released_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "slug_tombstones_slug_key" UNIQUE ("slug"), CONSTRAINT "slug_tombstones_released_by_user_id_fkey" FOREIGN KEY ("released_by_user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "slug_tombstones_slug_format_check" CHECK ((char_length(slug) >= 4) AND (char_length(slug) <= 30) AND (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text)));

-- +goose Down
-- reverse: create "slug_tombstones" table
DROP TABLE "public"."slug_tombstones";
-- reverse: create index "resumes_user_id_idx" to table: "resumes"
DROP INDEX "public"."resumes_user_id_idx";
-- reverse: create "resumes" table
DROP TABLE "public"."resumes";
-- reverse: create index "idempotency_records_expires_at_idx" to table: "idempotency_records"
DROP INDEX "public"."idempotency_records_expires_at_idx";
-- reverse: create "idempotency_records" table
DROP TABLE "public"."idempotency_records";
