-- +goose Up
-- modify "sessions" table
ALTER TABLE "public"."sessions" ADD COLUMN "rotated_from" uuid NULL, ADD CONSTRAINT "sessions_rotated_from_fkey" FOREIGN KEY ("rotated_from") REFERENCES "public"."sessions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- create index "sessions_rotated_from_key" to table: "sessions"
CREATE UNIQUE INDEX "sessions_rotated_from_key" ON "public"."sessions" ("rotated_from") WHERE (rotated_from IS NOT NULL);

-- +goose Down
-- reverse: create index "sessions_rotated_from_key" to table: "sessions"
DROP INDEX "public"."sessions_rotated_from_key";
-- reverse: modify "sessions" table
ALTER TABLE "public"."sessions" DROP CONSTRAINT "sessions_rotated_from_fkey", DROP COLUMN "rotated_from";
