import { defineConfig, devices } from "@playwright/test";
import path from "path";

const backendDir = path.resolve(__dirname, "../backend");
const fixtureDir = path.resolve(backendDir, "testdata");

export default defineConfig({
  testDir: "./e2e",
  timeout: 120_000,
  expect: {
    timeout: 20_000,
  },
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: process.env.E2E_BASE_URL || "http://localhost:3000",
    trace: "on-first-retry",
  },
  webServer: [
    {
      command: "go run ./cmd/api",
      cwd: backendDir,
      url: "http://localhost:8080/healthz",
      reuseExistingServer: true,
      timeout: 60_000,
      env: {
        ...process.env,
        DATABASE_URL: process.env.E2E_DATABASE_URL || "memory",
        RUN_EMBEDDED_WORKER: "true",
        WORKER_POLL_INTERVAL: "200ms",
        MOCK_GITHUB_FIXTURE_DIR: fixtureDir,
        API_ADDR: ":8080",
        ALLOWED_ORIGINS: "http://localhost:3000",
      },
    },
    {
      command: "npm run start",
      cwd: __dirname,
      url: "http://localhost:3000",
      reuseExistingServer: true,
      timeout: 60_000,
      env: {
        ...process.env,
        INTERNAL_API_BASE_URL: "http://localhost:8080",
        NEXT_PUBLIC_API_BASE_URL: "http://localhost:8080",
      },
    },
  ],
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        channel: process.env.CI ? undefined : "chrome",
      },
    },
  ],
});
