-- +goose Up
-- Sign in to view (docs/design/viewer-analytics/sign-in-to-view.md,
-- ADR 0022). A resume can require a viewer sign-in before it is public.
-- No viewer identity is stored anywhere; only the switch and a pass epoch
-- live on the resume, and the OAuth transaction table gains an
-- unauthenticated "view" purpose bound to a resume.
ALTER TABLE resumes
    ADD COLUMN sign_in_to_view boolean NOT NULL DEFAULT false,
    ADD COLUMN view_pass_epoch integer NOT NULL DEFAULT 0;

ALTER TABLE resumes
    ADD CONSTRAINT resumes_view_pass_epoch_check CHECK (view_pass_epoch >= 0);

ALTER TABLE oauth_transactions
    ADD COLUMN resume_id uuid NULL;

ALTER TABLE oauth_transactions
    ADD CONSTRAINT oauth_transactions_resume_id_fkey FOREIGN KEY (resume_id) REFERENCES resumes (id) ON UPDATE NO ACTION ON DELETE CASCADE;

ALTER TABLE oauth_transactions
    DROP CONSTRAINT oauth_transactions_purpose_check,
    DROP CONSTRAINT oauth_transactions_link_needs_user;

ALTER TABLE oauth_transactions
    ADD CONSTRAINT oauth_transactions_purpose_check CHECK (purpose = ANY (ARRAY['login'::text, 'link'::text, 'reauth'::text, 'view'::text])),
    ADD CONSTRAINT oauth_transactions_link_needs_user CHECK (
        ((purpose = ANY (ARRAY['link'::text, 'reauth'::text])) AND (linking_user_id IS NOT NULL))
        OR ((purpose = ANY (ARRAY['login'::text, 'view'::text])) AND (linking_user_id IS NULL))
    ),
    ADD CONSTRAINT oauth_transactions_view_resume_check CHECK (
        ((purpose = 'view'::text) AND (resume_id IS NOT NULL))
        OR ((purpose <> 'view'::text) AND (resume_id IS NULL))
    );

-- +goose Down
DELETE FROM oauth_transactions WHERE purpose = 'view';

ALTER TABLE oauth_transactions
    DROP CONSTRAINT oauth_transactions_view_resume_check,
    DROP CONSTRAINT oauth_transactions_link_needs_user,
    DROP CONSTRAINT oauth_transactions_purpose_check;

ALTER TABLE oauth_transactions
    ADD CONSTRAINT oauth_transactions_purpose_check CHECK (purpose = ANY (ARRAY['login'::text, 'link'::text, 'reauth'::text])),
    ADD CONSTRAINT oauth_transactions_link_needs_user CHECK (((purpose = ANY (ARRAY['link'::text, 'reauth'::text])) AND (linking_user_id IS NOT NULL)) OR ((purpose = 'login'::text) AND (linking_user_id IS NULL)));

ALTER TABLE oauth_transactions
    DROP CONSTRAINT oauth_transactions_resume_id_fkey,
    DROP COLUMN resume_id;

ALTER TABLE resumes
    DROP CONSTRAINT resumes_view_pass_epoch_check,
    DROP COLUMN view_pass_epoch,
    DROP COLUMN sign_in_to_view;
