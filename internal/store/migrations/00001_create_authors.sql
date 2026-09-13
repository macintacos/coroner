-- +goose Up
CREATE TABLE authors (
  id   INTEGER PRIMARY KEY,
  name text    NOT NULL,
  bio  text
);

-- +goose Down
DROP TABLE authors;
