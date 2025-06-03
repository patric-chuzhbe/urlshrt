-- +goose Up
-- +goose StatementBegin
CREATE TABLE short_to_full_url_map
(
    "full"     VARCHAR(255) NOT NULL,
    short      VARCHAR(255) NOT NULL,
    is_deleted BOOL         NOT NULL DEFAULT FALSE,
    CONSTRAINT PK_SHORT_TO_FULL_URL_MAP PRIMARY KEY ("full")
);

CREATE UNIQUE INDEX uq_short ON short_to_full_url_map (short);

CREATE TABLE USERS
(
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    CONSTRAINT PK_USERS PRIMARY KEY (id)
);

CREATE TABLE users_urls
(
    user_id UUID         NOT NULL,
    url     VARCHAR(255) NOT NULL,
    CONSTRAINT PK_USERS_URLS PRIMARY KEY (user_id, url)
);

ALTER TABLE users_urls
    ADD CONSTRAINT FK_USERS_UR_REFERENCE_USERS FOREIGN KEY (user_id)
        REFERENCES users (id)
        ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE users_urls
    ADD CONSTRAINT FK_USERS_UR_REFERENCE_SHORT_TO FOREIGN KEY (url)
        REFERENCES short_to_full_url_map ("full")
        ON DELETE CASCADE ON UPDATE CASCADE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users_urls DROP CONSTRAINT FK_USERS_UR_REFERENCE_USERS;
ALTER TABLE users_urls DROP CONSTRAINT FK_USERS_UR_REFERENCE_SHORT_TO;

DROP TABLE users_urls;
DROP TABLE users;
DROP TABLE short_to_full_url_map;
-- +goose StatementEnd
