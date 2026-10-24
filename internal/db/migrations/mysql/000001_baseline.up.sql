-- CreateTable
CREATE TABLE `Registration` (
    `id` VARCHAR(191) NOT NULL,
    `taxId` VARCHAR(191) NOT NULL,
    `applicant` VARCHAR(191) NOT NULL,
    `contactAddr` TEXT NOT NULL,
    `companyAddr` TEXT NOT NULL,
    `phone` VARCHAR(191) NOT NULL,
    `note` TEXT NOT NULL,
    `createdAt` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updatedAt` DATETIME(3) NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Certificate` (
    `id` VARCHAR(191) NOT NULL,
    `registrationId` VARCHAR(191) NOT NULL,
    `validDate` VARCHAR(191) NOT NULL,
    `status` VARCHAR(191) NOT NULL,
    `productName` TEXT NOT NULL,
    `soldAs` TEXT NOT NULL,
    `mainModel` VARCHAR(191) NOT NULL,
    `seriesModels` TEXT NOT NULL,
    `issuer` VARCHAR(191) NOT NULL,
    `createdAt` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updatedAt` DATETIME(3) NOT NULL,

    INDEX `Certificate_registrationId_idx`(`registrationId`),
    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Authorization` (
    `id` VARCHAR(191) NOT NULL,
    `certificateId` VARCHAR(191) NOT NULL,
    `authorizerName` VARCHAR(191) NOT NULL,
    `mainModel` VARCHAR(191) NOT NULL,
    `authorizeeTaxId` VARCHAR(191) NOT NULL,
    `authorizeeName` VARCHAR(191) NOT NULL,
    `authorizeeAddr` TEXT NOT NULL,
    `authorizeePhone` VARCHAR(191) NOT NULL,
    `validDate` VARCHAR(191) NOT NULL,
    `createdAt` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updatedAt` DATETIME(3) NOT NULL,

    INDEX `Authorization_certificateId_idx`(`certificateId`),
    INDEX `Authorization_authorizeeTaxId_idx`(`authorizeeTaxId`),
    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- AddForeignKey
ALTER TABLE `Certificate` ADD CONSTRAINT `Certificate_registrationId_fkey` FOREIGN KEY (`registrationId`) REFERENCES `Registration`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;
