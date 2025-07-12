-- name: RemoveUsersUrls :exec
UPDATE url_redirects
    SET is_deleted = true
    FROM users_urls
    WHERE url_redirects.original_url = users_urls.url
        AND users_urls.user_id = sqlc.arg(user_id)
        AND url_redirects.short = sqlc.arg(short_url);

-- name: SaveUserUrl :exec
INSERT INTO users_urls (user_id, url)
    VALUES (sqlc.arg(user_id), sqlc.arg(url))
    ON CONFLICT (user_id, url) DO UPDATE
        SET
            user_id = EXCLUDED.user_id,
            url = EXCLUDED.url;

-- name: GetUserUrls :many
SELECT url_redirects.original_url, url_redirects.short
    FROM url_redirects
        JOIN users_urls ON
            users_urls.url = url_redirects.original_url
                AND users_urls.user_id = sqlc.arg(user_id)
                AND NOT url_redirects.is_deleted;

-- name: CreateUser :one
INSERT INTO users DEFAULT VALUES
    RETURNING user_id;

-- name: GetUserByID :one
SELECT user_id
    FROM users
    WHERE user_id = sqlc.arg(user_id);

-- name: SaveURLMapping :exec
INSERT INTO url_redirects (short, original_url)
    VALUES (sqlc.arg(short), sqlc.arg(original_url))
    ON CONFLICT DO NOTHING;

-- name: FindShortsByFulls :many
SELECT short, original_url
    FROM url_redirects
    WHERE original_url = ANY(sqlc.arg(original_urls)::text[]);

-- name: InsertURLMapping :exec
INSERT INTO url_redirects (short, original_url)
    VALUES (sqlc.arg(short), sqlc.arg(original_url));

-- name: FindFullByShort :one
SELECT original_url, is_deleted
    FROM url_redirects
    WHERE short = sqlc.arg(short);

-- name: FindShortByFull :one
SELECT short
    FROM url_redirects
    WHERE original_url = sqlc.arg(original_url);

-- name: IsShortExists :one
SELECT EXISTS (
    SELECT 1 FROM url_redirects WHERE short = sqlc.arg(short)
);

-- name: ResetDB :exec
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public') LOOP
        EXECUTE 'DROP TABLE IF EXISTS ' || quote_ident(r.tablename) || ' CASCADE';
    END LOOP;
END $$;

-- name: GetNumberOfShortenedURLs :one
SELECT COUNT(*) FROM url_redirects WHERE NOT is_deleted;

-- name: GetNumberOfUsers :one
SELECT COUNT(*) FROM users;
