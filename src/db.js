import { PrismaClient } from "./generated/prisma/client.ts";

let prisma;

if (process.env.NODE_ENV === "production") {
  const datasourceUrl =
    process.env.DATABASE_URL ||
    `mysql://${process.env.MYSQL_USER}:${process.env.MYSQL_PASSWORD}@${process.env.MYSQL_HOST}:${process.env.MYSQL_PORT}/${process.env.MYSQL_DATABASE}`;
  prisma = new PrismaClient({ datasourceUrl });
} else {
  const { PrismaBetterSqlite3 } = await import("@prisma/adapter-better-sqlite3");
  const url = process.env.DATABASE_URL || "file:./prisma/dev.db";
  const adapter = new PrismaBetterSqlite3({ url });
  prisma = new PrismaClient({ adapter });
}

export default prisma;
