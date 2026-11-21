-- Every vote is lost, decided or not. The orders votes became stay: they are
-- ordinary orders and never depended on the vote.
DROP TABLE vote_ballot;
ALTER TABLE vote DROP CONSTRAINT vote_winner_is_own_choice;
DROP TABLE vote_choice;
DROP TABLE vote;
DROP FUNCTION vote_choice_count_check();
