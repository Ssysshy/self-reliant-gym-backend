-- 计划操作编号与唯一约束。列和索引分别判断，保证中断后重跑不会撞 1060 / 1061。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'plans' AND COLUMN_NAME = 'operation_id') = 0,
  'ALTER TABLE plans ADD COLUMN operation_id VARCHAR(72) NULL', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'plans' AND INDEX_NAME = 'uq_plan_operation') = 0,
  'ALTER TABLE plans ADD UNIQUE INDEX uq_plan_operation (owner, operation_id)', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
