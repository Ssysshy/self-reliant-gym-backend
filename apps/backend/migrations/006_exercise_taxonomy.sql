-- 动作库按「部位 / 细分肌群 / 器材分类」组织：左侧按部位浏览，顶部按器材分类筛选，卡片上标细分肌群。
-- 逐列判断后再加，保证中断重跑不会撞 1060 Duplicate column。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exercises' AND COLUMN_NAME = 'sub_muscle') = 0,
  CONCAT('ALTER TABLE exercises ADD COLUMN sub_muscle VARCHAR(32) NOT NULL DEFAULT ', CHAR(39), CHAR(39)), 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exercises' AND COLUMN_NAME = 'category') = 0,
  CONCAT('ALTER TABLE exercises ADD COLUMN category VARCHAR(32) NOT NULL DEFAULT ', CHAR(39), CHAR(39)), 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
-- 旧数据的部位带「部」字尾，统一成侧边栏短名，避免同一格出现两种写法。
UPDATE exercises SET muscle = CASE muscle WHEN '胸部' THEN '胸' WHEN '背部' THEN '背' WHEN '腿部' THEN '腿' WHEN '肩部' THEN '肩' ELSE muscle END WHERE muscle IN ('胸部', '背部', '腿部', '肩部');
-- 旧数据没有器材分类：先按器材名认出来，剩下的都是器械。
UPDATE exercises SET category = CASE WHEN equipment = '哑铃' THEN '哑铃' WHEN equipment = '杠铃' THEN '杠铃' WHEN equipment = '弹力带' THEN '弹力带' WHEN equipment IN ('训练垫', '训练凳', '单杠', '双杠') THEN '徒手' ELSE '器械' END WHERE category = '';
-- 早期示范动作只标了大部位，这里按动作名补齐细分，动作库左侧第二层才不会是空的。
UPDATE exercises SET sub_muscle = CASE name WHEN '坐姿推胸' THEN '中胸' WHEN '坐姿划船' THEN '中背' WHEN '腿举' THEN '股四头' WHEN '杯式深蹲' THEN '股四头' WHEN '高位下拉' THEN '背阔肌' WHEN '器械推肩' THEN '前束' WHEN '坐姿腿弯举' THEN '腘绳肌' ELSE sub_muscle END WHERE sub_muscle = '';
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exercises' AND INDEX_NAME = 'idx_exercises_part') = 0,
  'CREATE INDEX idx_exercises_part ON exercises (muscle, sub_muscle)', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
