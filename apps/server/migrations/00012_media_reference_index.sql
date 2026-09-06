-- +goose Up
CREATE INDEX resumes_photo_reference_idx
    ON resumes ((personal_details -> 'photo' ->> 'key'))
    WHERE personal_details -> 'photo' ->> 'key' IS NOT NULL;

-- +goose Down
DROP INDEX resumes_photo_reference_idx;
