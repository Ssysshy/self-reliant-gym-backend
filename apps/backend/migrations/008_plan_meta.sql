-- 模板展示信息：封面、难度、参考时长、参考消耗，以及详情页的分段文案（JSON）。
-- 只有官方模板（owner=0）会填，用户自己存的计划这一列为 NULL。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'plans' AND COLUMN_NAME = 'meta') = 0,
  'ALTER TABLE plans ADD COLUMN meta JSON NULL', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
