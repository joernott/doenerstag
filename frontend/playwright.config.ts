// Playwright: the tests that need a real browser and a real server.
//
// Everything that can be asserted in a headless DOM is a Vitest test; these are
// the flows where the browser is part of what is being tested -- two windows
// watching one order, a cookie surviving a reload, the keyboard walking a
// modal.
//
// The server is not started here. It is a Go binary with a database behind it,
// and starting it belongs to whoever knows where that database is: `make e2e`
// locally, the workflow in CI. The tests take its address from
// DOENER_E2E_URL.

import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env["DOENER_E2E_URL"] ?? "https://localhost:8443";

export default defineConfig({
  testDir: "./e2e",
  // The suite creates the world it needs through the API, so two files running
  // at once would be two suites creating restaurants in the same database.
  // Serial is also honest about the machine this runs on: two cores.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env["CI"],
  retries: 0,
  reporter: process.env["CI"] ? "github" : "list",
  timeout: 30_000,
  expect: { timeout: 10_000 },

  use: {
    baseURL,
    // A development server uses a self-signed certificate, and refusing it
    // would only test that the certificate is self-signed.
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  // Both browsers docs/12_testing.md names. They are the two this application
  // is designed for, and they disagree about enough -- date inputs, focus
  // handling, EventSource buffering -- to be worth running both.
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
  ],
});
