-- Votes for the restaurant (22.5, F12, ADR-0013).
--
-- Three tables: the vote, the restaurants it offers, and one ballot per
-- voter. A vote has no row in food_order until it is decided; then the order
-- it became is an ordinary one and the vote points at it, going with it when
-- the order is deleted.
--
-- The rules a racing pair of requests could break are the schema's: one
-- ballot per voter is the primary key, a ballot naming another vote's choice
-- is a composite foreign key, a chosen restaurant cannot be removed because
-- the ballot's foreign key refuses it, and two to five choices is a deferred
-- constraint trigger that locks the vote before it counts.

CREATE TABLE vote (
    id                uuid        PRIMARY KEY,
    creator_id        uuid        NOT NULL
                                  DEFAULT '00000000-0000-7000-8000-000000000000'
                                  REFERENCES app_user (id) ON DELETE SET DEFAULT,
    decision_at       timestamptz NOT NULL,
    deadline_at       timestamptz NOT NULL,
    fulfilment_at     timestamptz NOT NULL,
    open_choices      boolean     NOT NULL DEFAULT false,
    public_ballots    boolean     NOT NULL DEFAULT false,
    decided           boolean     NOT NULL DEFAULT false,
    outcome           text,
    winner_choice_id  uuid,
    order_id          uuid        UNIQUE REFERENCES food_order (id) ON DELETE CASCADE,

    created_at        timestamptz NOT NULL DEFAULT now(),
    created_by        uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    updated_by        uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT vote_times_in_order
        CHECK (decision_at < deadline_at AND deadline_at < fulfilment_at),
    CONSTRAINT vote_outcome_known
        CHECK (outcome IN ('ordered', 'no_ballots', 'too_late')),
    CONSTRAINT vote_decided_has_outcome
        CHECK (decided = (outcome IS NOT NULL)),
    -- An order and a winner exactly when the outcome is an order.
    CONSTRAINT vote_ordered_has_order
        CHECK ((outcome = 'ordered') = (order_id IS NOT NULL AND winner_choice_id IS NOT NULL))
);

COMMENT ON TABLE vote IS
    'A vote for the restaurant of an order not made yet (F12). Decided once, by the server; goes with the order it became.';

-- The decision pass and the start page read the undecided votes by time.
CREATE INDEX vote_undecided_idx ON vote (decision_at) WHERE NOT decided;
-- Account deletion finds a user's votes through the foreign key.
CREATE INDEX vote_creator_idx ON vote (creator_id);

CREATE TRIGGER vote_set_updated_at
    BEFORE UPDATE ON vote
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE vote_choice (
    id             uuid        PRIMARY KEY,
    vote_id        uuid        NOT NULL REFERENCES vote (id) ON DELETE CASCADE,
    restaurant_id  uuid        NOT NULL REFERENCES restaurant (id) ON DELETE RESTRICT,
    added_by_id    uuid        NOT NULL
                               DEFAULT '00000000-0000-7000-8000-000000000000'
                               REFERENCES app_user (id) ON DELETE SET DEFAULT,
    final_count    integer,

    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT vote_choice_once_per_vote UNIQUE (vote_id, restaurant_id),
    -- The target of the ballot's and the winner's composite foreign keys.
    CONSTRAINT vote_choice_of_vote UNIQUE (vote_id, id),
    CONSTRAINT vote_choice_final_count_not_negative CHECK (final_count >= 0)
);

COMMENT ON TABLE vote_choice IS
    'A restaurant a vote offers, two to five per vote. final_count is set at the decision.';

CREATE INDEX vote_choice_restaurant_idx ON vote_choice (restaurant_id);
CREATE INDEX vote_choice_added_by_idx ON vote_choice (added_by_id);

CREATE TRIGGER vote_choice_set_updated_at
    BEFORE UPDATE ON vote_choice
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The winner is one of the vote's own choices.
ALTER TABLE vote
    ADD CONSTRAINT vote_winner_is_own_choice
    FOREIGN KEY (id, winner_choice_id) REFERENCES vote_choice (vote_id, id);

CREATE TABLE vote_ballot (
    vote_id     uuid        NOT NULL REFERENCES vote (id) ON DELETE CASCADE,
    user_id     uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    choice_id   uuid        NOT NULL,
    cast_at     timestamptz NOT NULL DEFAULT now(),

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    PRIMARY KEY (vote_id, user_id),
    -- NO ACTION rather than RESTRICT: it is checked at the end of the
    -- statement, so deleting the vote, which cascades to its choices and its
    -- ballots alike, goes through, while removing a chosen restaurant on its
    -- own is refused.
    CONSTRAINT vote_ballot_choice_of_vote
        FOREIGN KEY (vote_id, choice_id) REFERENCES vote_choice (vote_id, id)
);

COMMENT ON TABLE vote_ballot IS
    'One ballot per voter. Holds who chose what whatever the vote''s setting; secrecy is a promise about responses, not storage.';

CREATE INDEX vote_ballot_choice_idx ON vote_ballot (vote_id, choice_id);
CREATE INDEX vote_ballot_user_idx ON vote_ballot (user_id);

CREATE TRIGGER vote_ballot_set_updated_at
    BEFORE UPDATE ON vote_ballot
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Two to five choices per vote (F12.3).
--
-- Checked at commit, because a vote and its choices are inserted one row at a
-- time. The vote row is locked before counting: two transactions adding a
-- fifth and a sixth restaurant at once would otherwise each count their own
-- five and both commit. Under READ COMMITTED the count after the lock sees
-- whatever the first transaction committed. NO KEY UPDATE rather than UPDATE:
-- each transaction already holds a KEY SHARE lock on the vote through its
-- choice's foreign key, which UPDATE would wait for, deadlocking the pair;
-- NO KEY UPDATE does not, and still makes the second wait for the first.
-- A vote that is itself gone --
-- deleted, cascading to its choices -- is not checked.
CREATE FUNCTION vote_choice_count_check() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    target uuid;
    choices integer;
BEGIN
    IF TG_TABLE_NAME = 'vote' THEN
        target := NEW.id;
    ELSIF TG_OP = 'DELETE' THEN
        target := OLD.vote_id;
    ELSE
        target := NEW.vote_id;
    END IF;

    PERFORM 1 FROM vote WHERE id = target FOR NO KEY UPDATE;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    SELECT count(*) INTO choices FROM vote_choice WHERE vote_id = target;
    IF choices < 2 OR choices > 5 THEN
        RAISE EXCEPTION 'vote % offers % restaurants; a vote offers two to five', target, choices
            USING ERRCODE = 'check_violation', CONSTRAINT = 'vote_choice_count';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER vote_choice_count
    AFTER INSERT OR DELETE ON vote_choice
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION vote_choice_count_check();

CREATE CONSTRAINT TRIGGER vote_choice_count_on_create
    AFTER INSERT ON vote
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION vote_choice_count_check();

