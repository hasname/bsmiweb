-- CreateTable
CREATE TABLE `registration` (
    `id` TEXT NOT NULL,
    `tax_id` TEXT NOT NULL,
    `applicant` TEXT NOT NULL,
    `contact_addr` TEXT NOT NULL,
    `company_addr` TEXT NOT NULL,
    `phone` TEXT NOT NULL,
    `note` TEXT NOT NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL,

    PRIMARY KEY (`id`)
);

-- CreateTable
CREATE TABLE `certificate` (
    `id` TEXT NOT NULL,
    `registration_id` TEXT NOT NULL,
    `valid_date` TEXT NOT NULL,
    `status` TEXT NOT NULL,
    `product_name` TEXT NOT NULL,
    `sold_as` TEXT NOT NULL,
    `main_model` TEXT NOT NULL,
    `series_models` TEXT NOT NULL,
    `issuer` TEXT NOT NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL,

    PRIMARY KEY (`id`),
    FOREIGN KEY (`registration_id`) REFERENCES `registration`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE
);

-- CreateTable
CREATE TABLE `authorization` (
    `id` TEXT NOT NULL,
    `certificate_id` TEXT NOT NULL,
    `authorizer_name` TEXT NOT NULL,
    `main_model` TEXT NOT NULL,
    `authorizee_tax_id` TEXT NOT NULL,
    `authorizee_name` TEXT NOT NULL,
    `authorizee_addr` TEXT NOT NULL,
    `authorizee_phone` TEXT NOT NULL,
    `valid_date` TEXT NOT NULL,
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME NOT NULL,

    PRIMARY KEY (`id`)
);

CREATE INDEX certificate_registration_id_idx ON certificate(registration_id);
CREATE INDEX authorization_certificate_id_idx ON authorization(certificate_id);
CREATE INDEX authorization_authorizee_tax_id_idx ON authorization(authorizee_tax_id);
