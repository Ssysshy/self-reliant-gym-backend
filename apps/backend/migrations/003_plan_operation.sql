ALTER TABLE plans ADD COLUMN operation_id VARCHAR(72) NULL, ADD UNIQUE INDEX uq_plan_operation (owner,operation_id);
