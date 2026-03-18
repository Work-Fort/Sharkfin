-- 008_auth_id.sql
-- Adds auth_id column to decouple internal ID from Passport UUID.
ALTER TABLE identities ADD COLUMN auth_id TEXT;
UPDATE identities SET auth_id = id WHERE auth_id IS NULL;
CREATE UNIQUE INDEX idx_identities_auth_id ON identities(auth_id);
