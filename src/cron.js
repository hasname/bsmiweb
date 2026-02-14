import schedule from "node-schedule";

schedule.scheduleJob("*/5 * * * *", () => {
  console.log("Cron job triggered");
});

console.log("Cron started");
