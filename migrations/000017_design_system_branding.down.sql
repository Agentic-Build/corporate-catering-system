-- 000017_design_system_branding.down.sql
-- 反向移除 000017 新增的 vendor 品牌欄位與 cuisine_category 表。

DROP INDEX IF EXISTS vendor_cuisine_category_idx;
ALTER TABLE vendor
  DROP COLUMN IF EXISTS price_level,
  DROP COLUMN IF EXISTS rating_count,
  DROP COLUMN IF EXISTS rating,
  DROP COLUMN IF EXISTS logo_uri,
  DROP COLUMN IF EXISTS cover_image_uri,
  DROP COLUMN IF EXISTS cuisine_category_id;

DROP TABLE IF EXISTS cuisine_category;
