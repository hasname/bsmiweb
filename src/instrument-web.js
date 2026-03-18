import * as Sentry from "@sentry/node";

if (process.env.SENTRY_DSN_WEB) {
  Sentry.init({
    dsn: process.env.SENTRY_DSN_WEB,
    tracesSampleRate: 0.1,
  });
}
