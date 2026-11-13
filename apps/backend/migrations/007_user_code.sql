-- 用户编号落库：形如 U3M8Q2K，由用户 ID 派生（见 internal/handler/code.go），
-- 注册时写入，存量用户由程序启动时的回填补齐。唯一索引让「按编号查人」不会撞车。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'code') = 0,
  'ALTER TABLE users ADD COLUMN code VARCHAR(12) NULL', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'idx_user_code') = 0,
  'ALTER TABLE users ADD UNIQUE INDEX idx_user_code (code)', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
