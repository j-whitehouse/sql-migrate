-- +migrate Up
INSERT INTO people (id) VALUES (5), (6), (7);

-- +migrate Down
DELETE FROM people WHERE id=5;
DELETE FROM people WHERE id=6;
DELETE FROM people WHERE id=7;
