-- 首页鼓励弹窗：后台维护文案，小程序每天最多弹两次。
-- enabled 控制是否参与随机的「启用」集合；sort 是下标顺序，值小的排在前面（后台列表与小程序随机池都按它）。
CREATE TABLE IF NOT EXISTS encouragements (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  `text` VARCHAR(160) NOT NULL,
  author VARCHAR(32) NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  `sort` INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  INDEX idx_encouragement_enabled (`enabled`, `sort`, id)
);

-- 预置几句开箱即用的文案：运营进后台就能看到成型的例子，不想用直接删掉即可。
-- 只在迁移时插入一次，后台清空后重启不会被塞回来。
INSERT INTO encouragements (`text`, author, enabled, `sort`, created_at) VALUES
  ('今天不必练到力竭，能来就已经赢了大多数人。', '撸铁不求人', TRUE, 10, NOW(3)),
  ('进步不是每次都加重量，而是每次都来。', '撸铁不求人', TRUE, 20, NOW(3)),
  ('你举起的不是铁，是明天更稳的自己。', '撸铁不求人', TRUE, 30, NOW(3)),
  ('状态不好就练轻一点，别把今天的缺席留给明天。', '撸铁不求人', TRUE, 40, NOW(3)),
  ('一组一组做完，回头看就是一周一个月。', '撸铁不求人', TRUE, 50, NOW(3)),
  ('慢一点没关系，别停就行。', '撸铁不求人', TRUE, 60, NOW(3));
