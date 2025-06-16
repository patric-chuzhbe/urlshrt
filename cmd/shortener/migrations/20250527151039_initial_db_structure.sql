-- +goose Up
-- +goose StatementBegin
CREATE TABLE url_redirects
(
    original_url VARCHAR(255) NOT NULL,
    short        VARCHAR(255) NOT NULL,
    is_deleted   BOOL         NOT NULL DEFAULT FALSE,
    CONSTRAINT PK_SHORT_TO_FULL_URL_MAP PRIMARY KEY (original_url)
);

CREATE UNIQUE INDEX uq_short ON url_redirects (short);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE url_redirects;
-- +goose StatementEnd
