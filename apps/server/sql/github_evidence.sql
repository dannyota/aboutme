-- name: GetGitHubConnection :one
SELECT user_id, github_user_id, login, connected_at, generation
FROM github_connections
WHERE user_id = $1;

-- name: GetGitHubConnectionForUpdate :one
SELECT user_id, github_user_id, login, connected_at, generation
FROM github_connections
WHERE user_id = $1
FOR UPDATE;

-- name: InstallGitHubConnection :one
INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation)
VALUES (sqlc.arg(user_id), sqlc.arg(github_user_id)::bigint, sqlc.arg(login)::text, sqlc.arg(connected_at)::timestamptz, 1)
ON CONFLICT (user_id) DO UPDATE
SET github_user_id = EXCLUDED.github_user_id,
    login = EXCLUDED.login,
    connected_at = EXCLUDED.connected_at,
    generation = github_connections.generation + 1
RETURNING user_id, github_user_id, login, connected_at, generation;

-- name: ClearGitHubConnection :one
INSERT INTO github_connections (user_id, generation)
VALUES (sqlc.arg(user_id), 1)
ON CONFLICT (user_id) DO UPDATE
SET github_user_id = NULL,
    login = NULL,
    connected_at = NULL,
    generation = github_connections.generation + 1
RETURNING generation;
