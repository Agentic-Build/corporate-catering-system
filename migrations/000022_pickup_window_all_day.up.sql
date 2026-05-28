-- Pickup is allowed all day; the 11:50-12:10 window was only a display hint that
-- misled employees into thinking collection was time-restricted. Relabel existing
-- supplies seeded with the old fixed window to "全天".
UPDATE meal_supply SET pickup_window = '全天' WHERE pickup_window = '11:50-12:10';
UPDATE meal_supply SET eta_label = '全天' WHERE eta_label = '11:50-12:10';
