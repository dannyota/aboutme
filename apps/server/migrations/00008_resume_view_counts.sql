-- +goose Up
-- Daily view counts per resume (docs/design/viewer-analytics/counting.md,
-- ADR 0061). Rows hold numbers only, never personal data. A day is an
-- Asia/Ho_Chi_Minh date. The privacy sweep deletes rows older than 400 days.
CREATE TABLE resume_view_days (
    resume_id uuid NOT NULL,
    day date NOT NULL,
    counted integer NOT NULL DEFAULT 0,
    bot integer NOT NULL DEFAULT 0,
    datacenter integer NOT NULL DEFAULT 0,
    anomaly integer NOT NULL DEFAULT 0,
    invalid integer NOT NULL DEFAULT 0,
    crawler integer NOT NULL DEFAULT 0,
    PRIMARY KEY (resume_id, day),
    CONSTRAINT resume_view_days_resume_id_fkey FOREIGN KEY (resume_id) REFERENCES resumes (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT resume_view_days_counts_check CHECK (
        counted >= 0 AND bot >= 0 AND datacenter >= 0
        AND anomaly >= 0 AND invalid >= 0 AND crawler >= 0
    )
);

CREATE INDEX resume_view_days_day_idx ON resume_view_days (day);

-- Link-preview fetches per platform per day: a share signal, not a view.
CREATE TABLE resume_share_signal_days (
    resume_id uuid NOT NULL,
    day date NOT NULL,
    platform text NOT NULL,
    fetches integer NOT NULL DEFAULT 0,
    PRIMARY KEY (resume_id, day, platform),
    CONSTRAINT resume_share_signal_days_resume_id_fkey FOREIGN KEY (resume_id) REFERENCES resumes (id) ON UPDATE NO ACTION ON DELETE CASCADE,
    CONSTRAINT resume_share_signal_days_platform_check CHECK (platform IN (
        'facebook', 'linkedin', 'x', 'telegram', 'whatsapp',
        'slack', 'discord', 'skype', 'viber', 'zalo'
    )),
    CONSTRAINT resume_share_signal_days_fetches_check CHECK (fetches >= 0)
);

CREATE INDEX resume_share_signal_days_day_idx ON resume_share_signal_days (day);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE resume_view_days TO aboutme_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE resume_share_signal_days TO aboutme_app;

-- +goose Down
DROP TABLE resume_share_signal_days;
DROP TABLE resume_view_days;
