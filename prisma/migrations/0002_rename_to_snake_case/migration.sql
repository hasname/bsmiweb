-- Drop old foreign key
ALTER TABLE `Certificate` DROP FOREIGN KEY `Certificate_registrationId_fkey`;

-- Rename tables from PascalCase to snake_case
RENAME TABLE `Registration` TO `registration`;
RENAME TABLE `Certificate` TO `certificate`;
RENAME TABLE `Authorization` TO `authorization`;

-- Rename columns in registration
ALTER TABLE `registration`
  CHANGE `taxId` `tax_id` VARCHAR(191) NOT NULL,
  CHANGE `contactAddr` `contact_addr` TEXT NOT NULL,
  CHANGE `companyAddr` `company_addr` TEXT NOT NULL,
  CHANGE `createdAt` `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  CHANGE `updatedAt` `updated_at` DATETIME(3) NOT NULL;

-- Rename columns in certificate
ALTER TABLE `certificate`
  CHANGE `registrationId` `registration_id` VARCHAR(191) NOT NULL,
  CHANGE `validDate` `valid_date` VARCHAR(191) NOT NULL,
  CHANGE `productName` `product_name` TEXT NOT NULL,
  CHANGE `soldAs` `sold_as` TEXT NOT NULL,
  CHANGE `mainModel` `main_model` VARCHAR(191) NOT NULL,
  CHANGE `seriesModels` `series_models` TEXT NOT NULL,
  CHANGE `createdAt` `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  CHANGE `updatedAt` `updated_at` DATETIME(3) NOT NULL;

-- Rename columns in authorization
ALTER TABLE `authorization`
  CHANGE `certificateId` `certificate_id` VARCHAR(191) NOT NULL,
  CHANGE `authorizerName` `authorizer_name` VARCHAR(191) NOT NULL,
  CHANGE `mainModel` `main_model` VARCHAR(191) NOT NULL,
  CHANGE `authorizeeTaxId` `authorizee_tax_id` VARCHAR(191) NOT NULL,
  CHANGE `authorizeeName` `authorizee_name` VARCHAR(191) NOT NULL,
  CHANGE `authorizeeAddr` `authorizee_addr` TEXT NOT NULL,
  CHANGE `authorizeePhone` `authorizee_phone` VARCHAR(191) NOT NULL,
  CHANGE `validDate` `valid_date` VARCHAR(191) NOT NULL,
  CHANGE `createdAt` `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  CHANGE `updatedAt` `updated_at` DATETIME(3) NOT NULL;

-- Rename indexes
ALTER TABLE `certificate` DROP INDEX `Certificate_registrationId_idx`;
CREATE INDEX `certificate_registration_id_idx` ON `certificate`(`registration_id`);

ALTER TABLE `authorization` DROP INDEX `Authorization_certificateId_idx`;
CREATE INDEX `authorization_certificate_id_idx` ON `authorization`(`certificate_id`);

ALTER TABLE `authorization` DROP INDEX `Authorization_authorizeeTaxId_idx`;
CREATE INDEX `authorization_authorizee_tax_id_idx` ON `authorization`(`authorizee_tax_id`);

-- Re-add foreign key with new names
ALTER TABLE `certificate` ADD CONSTRAINT `certificate_registration_id_fkey` FOREIGN KEY (`registration_id`) REFERENCES `registration`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;
