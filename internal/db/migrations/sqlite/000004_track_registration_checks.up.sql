-- Record BSMI change checks so refreshes can be scheduled by estimated change rate.
ALTER TABLE `registration` ADD COLUMN `checked_at` DATETIME;
ALTER TABLE `registration` ADD COLUMN `check_count` INTEGER NOT NULL DEFAULT 0;
ALTER TABLE `registration` ADD COLUMN `change_count` INTEGER NOT NULL DEFAULT 0;
ALTER TABLE `registration` ADD COLUMN `check_seconds` INTEGER NOT NULL DEFAULT 0;
