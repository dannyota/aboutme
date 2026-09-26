-- View count queries (docs/design/viewer-analytics/counting.md). Tables hold
-- daily numbers only; days are Asia/Ho_Chi_Minh dates.

-- name: GetLiveViewResumeBySlug :one
-- The same live gate as GetPublicResumeBySlug.
SELECT id, user_id FROM resumes
WHERE slug = sqlc.arg(slug)::text AND live = true;

-- name: GetLiveViewResumeByID :one
SELECT id, user_id FROM resumes
WHERE id = sqlc.arg(id)::uuid AND live = true;

-- name: AddResumeViewDays :exec
-- Target-list unnest zips the equal-length arrays row by row. The join
-- skips a resume deleted since the view; the cascade removes its rows
-- otherwise.
INSERT INTO resume_view_days AS d (resume_id, day, counted, bot, datacenter, anomaly, invalid, crawler)
SELECT cell.resume_id, cell.day, cell.counted, cell.bot, cell.datacenter, cell.anomaly, cell.invalid, cell.crawler
FROM (
    SELECT unnest(sqlc.arg(resume_ids)::uuid[]) AS resume_id,
           unnest(sqlc.arg(days)::date[]) AS day,
           unnest(sqlc.arg(counted)::int[]) AS counted,
           unnest(sqlc.arg(bot)::int[]) AS bot,
           unnest(sqlc.arg(datacenter)::int[]) AS datacenter,
           unnest(sqlc.arg(anomaly)::int[]) AS anomaly,
           unnest(sqlc.arg(invalid)::int[]) AS invalid,
           unnest(sqlc.arg(crawler)::int[]) AS crawler
) AS cell
JOIN resumes ON resumes.id = cell.resume_id
ORDER BY cell.resume_id, cell.day
ON CONFLICT (resume_id, day) DO UPDATE SET
    counted = d.counted + EXCLUDED.counted,
    bot = d.bot + EXCLUDED.bot,
    datacenter = d.datacenter + EXCLUDED.datacenter,
    anomaly = d.anomaly + EXCLUDED.anomaly,
    invalid = d.invalid + EXCLUDED.invalid,
    crawler = d.crawler + EXCLUDED.crawler;

-- name: AddResumeShareSignalDays :exec
INSERT INTO resume_share_signal_days AS s (resume_id, day, platform, fetches)
SELECT cell.resume_id, cell.day, cell.platform, cell.fetches
FROM (
    SELECT unnest(sqlc.arg(resume_ids)::uuid[]) AS resume_id,
           unnest(sqlc.arg(days)::date[]) AS day,
           unnest(sqlc.arg(platforms)::text[]) AS platform,
           unnest(sqlc.arg(fetches)::int[]) AS fetches
) AS cell
JOIN resumes ON resumes.id = cell.resume_id
ORDER BY cell.resume_id, cell.day, cell.platform
ON CONFLICT (resume_id, day, platform) DO UPDATE SET
    fetches = s.fetches + EXCLUDED.fetches;

-- name: ListViewSummaries :many
-- Every owned resume that is live or has counts, with totals over the last
-- 7, 30, and 90 days.
SELECT r.id, r.title, r.slug, r.live,
       COALESCE(sum(d.counted) FILTER (WHERE d.day >= sqlc.arg(since7)::date), 0)::bigint AS real7,
       COALESCE(sum(d.bot + d.datacenter + d.anomaly + d.invalid + d.crawler)
           FILTER (WHERE d.day >= sqlc.arg(since7)::date), 0)::bigint AS filtered7,
       COALESCE(sum(d.counted) FILTER (WHERE d.day >= sqlc.arg(since30)::date), 0)::bigint AS real30,
       COALESCE(sum(d.bot + d.datacenter + d.anomaly + d.invalid + d.crawler)
           FILTER (WHERE d.day >= sqlc.arg(since30)::date), 0)::bigint AS filtered30,
       COALESCE(sum(d.counted), 0)::bigint AS real90,
       COALESCE(sum(d.bot + d.datacenter + d.anomaly + d.invalid + d.crawler), 0)::bigint AS filtered90
FROM resumes AS r
LEFT JOIN resume_view_days AS d
    ON d.resume_id = r.id AND d.day >= sqlc.arg(since90)::date
WHERE r.user_id = sqlc.arg(user_id)::uuid
  AND (r.live OR EXISTS (SELECT 1 FROM resume_view_days AS any_day WHERE any_day.resume_id = r.id))
GROUP BY r.id
ORDER BY r.created_at DESC, r.id DESC
LIMIT 1000;

-- name: GetOwnedViewResume :one
SELECT id, title, slug, live FROM resumes
WHERE id = sqlc.arg(id)::uuid AND user_id = sqlc.arg(user_id)::uuid;

-- name: ListResumeViewDays :many
SELECT day, counted, bot, datacenter, anomaly, invalid, crawler
FROM resume_view_days
WHERE resume_id = sqlc.arg(resume_id)::uuid AND day >= sqlc.arg(since)::date
ORDER BY day;

-- name: ListResumeShareSignals :many
SELECT platform, sum(fetches)::bigint AS fetches
FROM resume_share_signal_days
WHERE resume_id = sqlc.arg(resume_id)::uuid AND day >= sqlc.arg(since)::date
GROUP BY platform
HAVING sum(fetches) > 0
ORDER BY sum(fetches) DESC, platform;

-- name: DeleteOldResumeViewDaysPage :execrows
-- Daily counts are kept 400 days (docs/design/viewer-analytics/README.md).
WITH candidates AS MATERIALIZED (
    SELECT resume_id, day
    FROM resume_view_days
    WHERE day < sqlc.arg(cutoff)::date
    ORDER BY day, resume_id
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM resume_view_days AS d
USING candidates
WHERE d.resume_id = candidates.resume_id AND d.day = candidates.day;

-- name: DeleteOldResumeShareSignalDaysPage :execrows
WITH candidates AS MATERIALIZED (
    SELECT resume_id, day, platform
    FROM resume_share_signal_days
    WHERE day < sqlc.arg(cutoff)::date
    ORDER BY day, resume_id, platform
    LIMIT LEAST(sqlc.arg(limit_rows)::int, 1000)
    FOR UPDATE SKIP LOCKED
)
DELETE FROM resume_share_signal_days AS s
USING candidates
WHERE s.resume_id = candidates.resume_id AND s.day = candidates.day
  AND s.platform = candidates.platform;
