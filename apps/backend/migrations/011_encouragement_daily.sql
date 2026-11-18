-- 每日一条：给文案加上「被当作今日推送的那一天」。
-- served_on 为 NULL = 未推送；等于今天 = 今日推送；早于今天 = 已推送（历史，不再参与抽取）。
-- 状态不另存一列，读的时候按 served_on 和今天的关系现算，省掉一次「第二天补写状态」的定时任务。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'encouragements' AND COLUMN_NAME = 'served_on') = 0,
  'ALTER TABLE encouragements ADD COLUMN served_on DATE NULL', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
-- 并发兜底：一天只能有一条今日推送。MySQL 的唯一索引允许多行 NULL，
-- 所以「未推送」的文案可以有很多条，今天的位子只有一个。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'encouragements' AND INDEX_NAME = 'uk_encouragement_served') = 0,
  'ALTER TABLE encouragements ADD UNIQUE INDEX uk_encouragement_served (served_on)', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
