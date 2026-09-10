-- How many minor units make one major unit, which is not always a power of ten.
--
-- `minor_unit` says how many decimal digits a currency is written with, and
-- everything so far has assumed that the divisor follows from it: two digits
-- means a hundred cents to the euro. For almost every currency in circulation
-- that is true, and for two of them it is not.
--
-- The Malagasy ariary divides into five iraimbilanja, and the Mauritanian
-- ouguiya into five khoums. They are the only non-decimal currencies left in
-- the world. Writing them with one decimal digit and dividing by ten is wrong
-- in a way that looks nearly right: ten iraimbilanja become one ariary and
-- should be two.
--
-- So the divisor becomes a column of its own rather than something derived. For
-- every existing row it is exactly what was derived before, which is why the
-- backfill is 10^minor_unit and why nothing that was correct changes.

ALTER TABLE currency
    ADD COLUMN minor_per_major integer NOT NULL DEFAULT 100;

UPDATE currency SET minor_per_major = (10::numeric ^ minor_unit)::integer;

ALTER TABLE currency
    ADD CONSTRAINT currency_minor_per_major_positive
        CHECK (minor_per_major >= 1);

COMMENT ON COLUMN currency.minor_unit IS
    'Decimal digits an amount is written with: 2 for EUR, 0 for JPY, 1 for MGA.';
COMMENT ON COLUMN currency.minor_per_major IS
    'Minor units in one major unit: 100 for EUR, 1 for JPY, 5 for MGA and MRU. Not always 10^minor_unit.';

-- The two currencies that motivated the column, so that the case is reachable
-- rather than theoretical. One decimal digit: an ariary divides into five, so
-- the amounts that exist are .0, .2, .4, .6 and .8.
INSERT INTO currency (code, symbol, minor_unit, minor_per_major, sort_order) VALUES
    ('MGA', 'Ar', 1, 5, 110),
    ('MRU', 'UM', 1, 5, 120)
ON CONFLICT (code) DO NOTHING;
