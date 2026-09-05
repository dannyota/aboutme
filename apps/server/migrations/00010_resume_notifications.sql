-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION notify_resume_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    changed resumes%ROWTYPE;
    next_revision bigint;
BEGIN
    IF TG_OP = 'DELETE' THEN
        changed := OLD;
        next_revision := CASE WHEN OLD.revision = 9223372036854775807
            THEN OLD.revision ELSE OLD.revision + 1 END;
    ELSE
        changed := NEW;
        next_revision := NEW.revision;
    END IF;
    PERFORM pg_notify('aboutme_resume_revision', json_build_object(
        'account_id', changed.user_id,
        'resume_id', changed.id,
        'revision', next_revision,
        'deleted', TG_OP = 'DELETE'
    )::text);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER resume_revision_notification
AFTER INSERT OR UPDATE OR DELETE ON resumes
FOR EACH ROW EXECUTE FUNCTION notify_resume_revision();

-- +goose Down
DROP TRIGGER resume_revision_notification ON resumes;
DROP FUNCTION notify_resume_revision();
