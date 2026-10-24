-- Record BSMI change checks so refreshes can be scheduled by estimated change rate.
ALTER TABLE `registration`
  ADD COLUMN `checked_at` DATETIME(3) NULL,
  ADD COLUMN `check_count` INT NOT NULL DEFAULT 0,
  ADD COLUMN `change_count` INT NOT NULL DEFAULT 0,
  ADD COLUMN `check_seconds` BIGINT NOT NULL DEFAULT 0;
