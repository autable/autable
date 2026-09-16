import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  workers: 1,
  expect: {
    timeout: 5_000
  },
  use: {
    baseURL: "http://127.0.0.1:5175",
    // Without retries "on-first-retry" never records anything; keep the
    // trace and a screenshot for failed tests so a CI-only failure can be
    // read back with its network responses.
    trace: "retain-on-failure",
    screenshot: "only-on-failure"
  },
  webServer: [
    {
      command: "node e2e/start-backend.mjs",
      url: "http://127.0.0.1:18081/api/metadata",
      reuseExistingServer: false,
      timeout: 30_000
    },
    {
      command: "npm run dev -- --port 5175 --strictPort",
      env: { AUTABLE_API_PROXY: "http://127.0.0.1:18081" },
      url: "http://127.0.0.1:5175",
      reuseExistingServer: false,
      timeout: 30_000
    }
  ],
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] }
    }
  ]
});
