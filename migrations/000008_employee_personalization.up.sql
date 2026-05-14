CREATE TABLE favorite_item (
  user_id      UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  menu_item_id UUID NOT NULL REFERENCES menu_item(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, menu_item_id)
);
CREATE INDEX favorite_item_user_idx ON favorite_item(user_id, created_at DESC);
