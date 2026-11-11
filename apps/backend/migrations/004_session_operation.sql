-- 一场训练一次提交：用 operation 号保证客户端重试不会写出两条记录。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sessions' AND COLUMN_NAME = 'operation_id') = 0,
  'ALTER TABLE sessions ADD COLUMN operation_id VARCHAR(72) NULL', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sessions' AND INDEX_NAME = 'uq_session_operation') = 0,
  'ALTER TABLE sessions ADD UNIQUE INDEX uq_session_operation (owner, operation_id)', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
