-- name: GetGitHubConnection :one
SELECT user_id, github_user_id, login, connected_at, generation, binding
FROM github_connections
WHERE user_id = $1;

-- name: GetGitHubConnectionForUpdate :one
SELECT user_id, github_user_id, login, connected_at, generation, binding
FROM github_connections
WHERE user_id = $1
FOR UPDATE;

-- name: InstallGitHubConnection :one
INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation, binding)
VALUES (sqlc.arg(user_id), sqlc.arg(github_user_id)::bigint, sqlc.arg(login)::text, sqlc.arg(connected_at)::timestamptz, 1, 1)
ON CONFLICT (user_id) DO UPDATE
SET github_user_id = EXCLUDED.github_user_id,
    login = EXCLUDED.login,
    connected_at = EXCLUDED.connected_at,
    generation = github_connections.generation + 1,
    binding = CASE
        WHEN github_connections.github_user_id IS NOT DISTINCT FROM EXCLUDED.github_user_id THEN github_connections.binding
        ELSE github_connections.binding + 1
    END
RETURNING user_id, github_user_id, login, connected_at, generation, binding;

-- name: ClearGitHubConnection :one
INSERT INTO github_connections (user_id, generation, binding)
VALUES (sqlc.arg(user_id), 1, 1)
ON CONFLICT (user_id) DO UPDATE
SET github_user_id = NULL,
    login = NULL,
    connected_at = NULL,
    generation = github_connections.generation + 1,
    binding = github_connections.binding + 1
RETURNING generation;

-- name: AccountHasProjectEvidence :one
SELECT EXISTS (
    SELECT 1
    FROM project_evidence e
    JOIN projects p ON p.id = e.project_id
    WHERE p.user_id = $1
);

-- name: ProjectBelongsToUser :one
SELECT EXISTS (
    SELECT 1 FROM projects WHERE id = sqlc.arg(project_id) AND user_id = sqlc.arg(user_id)
);
