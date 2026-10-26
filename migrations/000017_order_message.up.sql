-- The chat on an order (21.5).
--
-- One table, one row per message, tied to the order and removed with it. A
-- chat is what the people in an order say to each other about it -- "the food
-- is here", "who has change for a fifty" -- and has no life of its own once
-- the order is gone, which is why it cascades rather than outliving it.
--
-- The author follows order_item's rule for an account that goes away: the
-- message stays and passes to the deleted-user placeholder, so a conversation
-- does not lose a line from its middle. Unlike an item in an open order there
-- is nothing to recompute, so there is no reason to delete it instead.
--
-- Messages are not edited or deleted one by one. The audit columns are here
-- because every table carries them; updated_* will only ever move when an
-- account deletion hands a message to the placeholder.

CREATE TABLE order_message (
    id          uuid        PRIMARY KEY,
    order_id    uuid        NOT NULL REFERENCES food_order (id) ON DELETE CASCADE,
    user_id     uuid        NOT NULL
                            DEFAULT '00000000-0000-7000-8000-000000000000'
                            REFERENCES app_user (id) ON DELETE SET DEFAULT,
    body        text        NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT order_message_body_length
        CHECK (char_length(body) BETWEEN 1 AND 1000)
);

COMMENT ON TABLE order_message IS
    'The chat on an order. Removed with the order; an author''s deletion hands the message to the placeholder.';

-- An order's chat is read whole, oldest first; the id is a UUIDv7, so it
-- breaks a tie between two messages in the same microsecond in the order they
-- were written.
CREATE INDEX order_message_order_idx ON order_message (order_id, created_at, id);
-- Account deletion finds a user's messages through the foreign key.
CREATE INDEX order_message_user_idx ON order_message (user_id);

CREATE TRIGGER order_message_set_updated_at
    BEFORE UPDATE ON order_message
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
