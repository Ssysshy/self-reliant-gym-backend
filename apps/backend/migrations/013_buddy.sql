-- 首页右下角的「健身搭子」：一张人物图 + 几句台词，由后台维护，用户可以挑一个收藏。
-- 图片只存相对路径（/assets/... 或 CDN 上的路径），域名由前端拼 CDN 前缀。
-- quotes 是 JSON 数组，点一下搭子随机取一句；enabled 控制是否在小程序里出现。
CREATE TABLE IF NOT EXISTS buddies (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(32) NOT NULL DEFAULT '',
  image_url VARCHAR(512) NOT NULL,
  quotes JSON NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  `sort` INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  INDEX idx_buddy_enabled (`enabled`, `sort`, id)
);

-- 用户选中的搭子。0 表示没选过，小程序就展示列表第一个（users 表在 001_init.sql 建）
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'buddy_id') = 0,
  'ALTER TABLE users ADD COLUMN buddy_id BIGINT UNSIGNED NOT NULL DEFAULT 0', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;

-- 预置一个开箱可用的搭子：用的是小程序包里已有的黑白漫画人物图（assets/home/），
-- 不走 CDN 也能显示。不想用直接在后台删掉，删完首页右下角就不出现。
-- 只在表还是空的时候插：这份文件手工重跑也不会插出第二个。
INSERT INTO buddies (name, image_url, quotes, enabled, `sort`, created_at)
SELECT '小黑', '/assets/home/home-hero-bw.png', JSON_ARRAY(
    '今天这几组，没人替你练。',
    '重量可以慢慢加，人来了就别走。',
    '练完这一组，再喊累。',
    '别看我，看杠铃。'
  ), TRUE, 10, NOW(3)
FROM DUAL WHERE NOT EXISTS (SELECT 1 FROM buddies);
