module.exports = {
  apps: [
    {
      name: "bsmiweb",
      script: "src/index.js",
      instances: 2,
      autorestart: true,
      watch: false,
    },
    {
      name: "bsmiweb-cron",
      script: "src/cron.js",
      instances: 1,
      autorestart: true,
      watch: false,
    },
  ],
};
