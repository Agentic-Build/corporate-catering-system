-- Temporary sold-out flag: lets a vendor mark a supply unavailable for the day
-- without zeroing capacity (which would lose the planned number).
ALTER TABLE meal_supply ADD COLUMN sold_out BOOLEAN NOT NULL DEFAULT false;
