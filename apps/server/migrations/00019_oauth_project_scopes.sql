-- +goose Up
SET LOCAL lock_timeout = '5s';

ALTER TABLE oauth_grants DROP CONSTRAINT oauth_grants_scopes_check;
ALTER TABLE oauth_grants ADD CONSTRAINT oauth_grants_scopes_check CHECK (
    scopes = ANY (ARRAY[
        'resumes:read'::text,
        'resumes:write'::text,
        'resumes:read resumes:write'::text,
        'projects:read'::text,
        'resumes:read projects:read'::text,
        'resumes:write projects:read'::text,
        'resumes:read resumes:write projects:read'::text,
        'projects:write'::text,
        'resumes:read projects:write'::text,
        'resumes:write projects:write'::text,
        'resumes:read resumes:write projects:write'::text,
        'projects:read projects:write'::text,
        'resumes:read projects:read projects:write'::text,
        'resumes:write projects:read projects:write'::text,
        'resumes:read resumes:write projects:read projects:write'::text
    ])
);

ALTER TABLE oauth_authorization_codes DROP CONSTRAINT oauth_authorization_codes_scopes_check;
ALTER TABLE oauth_authorization_codes ADD CONSTRAINT oauth_authorization_codes_scopes_check CHECK (
    scopes = ANY (ARRAY[
        'resumes:read'::text,
        'resumes:write'::text,
        'resumes:read resumes:write'::text,
        'projects:read'::text,
        'resumes:read projects:read'::text,
        'resumes:write projects:read'::text,
        'resumes:read resumes:write projects:read'::text,
        'projects:write'::text,
        'resumes:read projects:write'::text,
        'resumes:write projects:write'::text,
        'resumes:read resumes:write projects:write'::text,
        'projects:read projects:write'::text,
        'resumes:read projects:read projects:write'::text,
        'resumes:write projects:read projects:write'::text,
        'resumes:read resumes:write projects:read projects:write'::text
    ])
);

-- +goose Down
SET LOCAL lock_timeout = '5s';

-- +goose StatementBegin
DO $$
BEGIN
    LOCK TABLE oauth_grants, oauth_authorization_codes IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (
        SELECT 1 FROM oauth_grants
        WHERE NOT (scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text]))
    ) OR EXISTS (
        SELECT 1 FROM oauth_authorization_codes
        WHERE NOT (scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text]))
    ) THEN
        RAISE EXCEPTION 'cannot remove project scopes while an OAuth grant or authorization code holds one';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE oauth_grants DROP CONSTRAINT oauth_grants_scopes_check;
ALTER TABLE oauth_grants ADD CONSTRAINT oauth_grants_scopes_check CHECK (
    scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text])
);

ALTER TABLE oauth_authorization_codes DROP CONSTRAINT oauth_authorization_codes_scopes_check;
ALTER TABLE oauth_authorization_codes ADD CONSTRAINT oauth_authorization_codes_scopes_check CHECK (
    scopes = ANY (ARRAY['resumes:read'::text, 'resumes:write'::text, 'resumes:read resumes:write'::text])
);
