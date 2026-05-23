-- 000017_design_system_branding.up.sql
-- 讓 schema 忠實承載 T-Bite 設計系統的品牌資料：
--   * 全域料理分類 cuisine_category（4 大分類，與每店的 menu_category 分區不同）
--   * vendor 新增封面/Logo/分類/評分/價位等品牌欄位（全部可為 NULL，不破壞既有 row）

-- 全域料理分類（台式/早餐/飲料甜點/日韓西式）。
-- id 直接沿用設計系統原始字串 id（'cat-taiwanese' 等），方便對照來源資料。
CREATE TABLE cuisine_category (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  short_name  TEXT,
  description TEXT,
  icon        TEXT,
  banner_uri  TEXT,
  sort_order  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX cuisine_category_sort_idx ON cuisine_category(sort_order);

-- vendor 品牌欄位。全部可為 NULL，既有 row 不受影響。
ALTER TABLE vendor
  ADD COLUMN cuisine_category_id TEXT REFERENCES cuisine_category(id),
  ADD COLUMN cover_image_uri     TEXT,
  ADD COLUMN logo_uri            TEXT,
  ADD COLUMN rating              NUMERIC(2,1),
  ADD COLUMN rating_count        INTEGER,
  ADD COLUMN price_level         TEXT;
CREATE INDEX vendor_cuisine_category_idx ON vendor(cuisine_category_id);

COMMENT ON TABLE cuisine_category IS 'Global cuisine categories (台式/早餐/飲料甜點/日韓西式) used for store browsing. Distinct from per-vendor menu_category sections.';
COMMENT ON COLUMN vendor.cuisine_category_id IS 'Which global cuisine category this vendor belongs to.';
COMMENT ON COLUMN vendor.cover_image_uri IS 'Store cover banner, e.g. /brand/stores/r001-cover.jpg.';
COMMENT ON COLUMN vendor.logo_uri IS 'Store logo, e.g. /brand/logos/r001.png.';
COMMENT ON COLUMN vendor.price_level IS 'Price level indicator from the design system, e.g. "$" / "$$".';
