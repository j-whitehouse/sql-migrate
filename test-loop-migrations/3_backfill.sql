-- +migrate Up
ALTER TABLE people ADD COLUMN first_name text;
-- +LoopBegin
UPDATE people SET first_name = 'Jim Bob' WHERE first_name IS NULL;
-- +ConditionalBegin
SELECT COUNT(*) FROM people WHERE first_name IS NULL;
-- +ConditionalEnd
-- +LoopEnd

-- +migrate Down
ALTER TABLE people DROP COLUMN first_name;