-- 会员档位可增删改：补上「套餐卡说明」与「微信虚拟支付商品 ID」两列。
-- product 留空表示继续用环境变量 XPAY_PRODUCT_<days>，存量三档不用改部署配置。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'products' AND COLUMN_NAME = 'note') = 0,
  'ALTER TABLE products ADD COLUMN note VARCHAR(64) NOT NULL DEFAULT ""', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'products' AND COLUMN_NAME = 'product') = 0,
  'ALTER TABLE products ADD COLUMN product VARCHAR(128) NOT NULL DEFAULT ""', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
-- 原本写死在小程序里的三档说明搬进库：以后改文案不用发版
UPDATE products SET note = '先体验全部会员功能' WHERE days = 7 AND note = '';
UPDATE products SET note = '覆盖一个完整训练周期' WHERE days = 30 AND note = '';
UPDATE products SET note = '全年不限次数生成' WHERE days = 365 AND note = '';
