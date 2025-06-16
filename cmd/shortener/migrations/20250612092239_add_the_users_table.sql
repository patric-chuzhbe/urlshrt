-- +goose Up
-- +goose StatementBegin
CREATE TABLE users
(
    user_id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    CONSTRAINT PK_USERS PRIMARY KEY (user_id)
);

CREATE TABLE users_urls
(
    user_id UUID         NOT NULL,
    url     VARCHAR(255) NOT NULL,
    CONSTRAINT PK_USERS_URLS PRIMARY KEY (user_id, url)
);

ALTER TABLE users_urls
    ADD CONSTRAINT FK_USERS_UR_REFERENCE_USERS FOREIGN KEY (user_id)
        REFERENCES users (user_id)
        ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE users_urls
    ADD CONSTRAINT FK_USERS_UR_REFERENCE_SHORT_TO FOREIGN KEY (url)
        REFERENCES url_redirects (original_url)
        ON DELETE CASCADE ON UPDATE CASCADE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users_urls DROP CONSTRAINT FK_USERS_UR_REFERENCE_USERS;
ALTER TABLE users_urls DROP CONSTRAINT FK_USERS_UR_REFERENCE_SHORT_TO;

DROP TABLE users_urls;
DROP TABLE users;
-- +goose StatementEnd
