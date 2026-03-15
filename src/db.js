import { PrismaClient } from "./generated/prisma/client.ts";

let prisma;

if (process.env.NODE_ENV === "production") {
  const { PrismaMariaDb } = await import("@prisma/adapter-mariadb");
  const adapter = new PrismaMariaDb({
    host: process.env.MYSQL_HOST,
    port: Number(process.env.MYSQL_PORT),
    user: process.env.MYSQL_USER,
    password: process.env.MYSQL_PASSWORD,
    database: process.env.MYSQL_DATABASE,
  });
  prisma = new PrismaClient({ adapter });
} else {
  const { PrismaBetterSqlite3 } = await import("@prisma/adapter-better-sqlite3");
  const url = process.env.DATABASE_URL || "file:./prisma/dev.db";
  const adapter = new PrismaBetterSqlite3({ url });
  prisma = new PrismaClient({ adapter });
}

export default prisma;
