import "dotenv/config";
import { defineConfig } from "prisma/config";

const isProduction = process.env["NODE_ENV"] === "production";

const user = encodeURIComponent(process.env["MYSQL_USER"] || "");
const password = encodeURIComponent(process.env["MYSQL_PASSWORD"] || "");
const host = process.env["MYSQL_HOST"];
const port = process.env["MYSQL_PORT"] || "3306";
const database = process.env["MYSQL_DATABASE"];

const databaseUrl =
  process.env["DATABASE_URL"] ||
  (host && database
    ? `mysql://${user}:${password}@${host}:${port}/${database}`
    : undefined);

export default defineConfig({
  schema: isProduction ? "prisma/schema.prisma" : "prisma/schema.dev.prisma",
  migrations: {
    path: "prisma/migrations",
  },
  datasource: {
    url: databaseUrl,
  },
});
