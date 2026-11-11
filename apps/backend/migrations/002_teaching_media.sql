-- 补充教学字段。逐列判断后再加，保证中断后重跑不会撞 1060 Duplicate column。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exercises' AND COLUMN_NAME = 'aux_muscles') = 0,
  CONCAT('ALTER TABLE exercises ADD COLUMN aux_muscles VARCHAR(255) NOT NULL DEFAULT ', CHAR(39), CHAR(39)), 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exercises' AND COLUMN_NAME = 'image_url') = 0,
  CONCAT('ALTER TABLE exercises ADD COLUMN image_url VARCHAR(1024) NOT NULL DEFAULT ', CHAR(39), CHAR(39)), 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exercises' AND COLUMN_NAME = 'mistakes') = 0,
  'ALTER TABLE exercises ADD COLUMN mistakes TEXT NULL', 'DO 0');
PREPARE gym_stmt FROM @ddl;
EXECUTE gym_stmt;
DEALLOCATE PREPARE gym_stmt;
