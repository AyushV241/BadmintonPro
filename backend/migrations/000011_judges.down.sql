-- Judge-mode sessions become player sessions.
ALTER TABLE sessions DROP COLUMN mode;
DROP TABLE judges;
