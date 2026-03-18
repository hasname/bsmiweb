import * as Sentry from "@sentry/node";

if (process.env.SENTRY_DSN_CRON) {
  Sentry.init({
    dsn: process.env.SENTRY_DSN_CRON,
    tracesSampleRate: 0.1,
  });
}
