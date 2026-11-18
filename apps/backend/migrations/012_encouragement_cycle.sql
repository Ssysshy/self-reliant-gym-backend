-- 轮次与历史拆成两列：
--   served_on   本轮用。NULL=未推送、=今天=今日推送、<今天=已推送。整轮推完会全部清回 NULL，开始新一轮。
--   last_served 历史用。记录这句最后一次被推送的日期，轮次清空时不跟着清，后台靠它显示「上次推送」，
--               抽取时也靠它避开昨天刚推过的那条。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'encouragements' AND COLUMN_NAME = 'last_served') = 0,
  'ALTER TABLE encouragements ADD COLUMN last_served DATE NULL', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
-- 存量数据回填：已经推过的，最后一次推送日期就是当时那个 served_on
UPDATE encouragements SET last_served = served_on WHERE served_on IS NOT NULL AND last_served IS NULL;
