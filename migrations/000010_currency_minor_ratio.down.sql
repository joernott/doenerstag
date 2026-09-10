-- The two non-decimal currencies go with the column that made them
-- representable. A restaurant priced in either would hold the schema back,
-- which is the right way round: dropping the column while an ariary price
-- exists would silently divide it by ten.
DELETE FROM currency WHERE code IN ('MGA', 'MRU');

ALTER TABLE currency
    DROP CONSTRAINT currency_minor_per_major_positive,
    DROP COLUMN minor_per_major;
