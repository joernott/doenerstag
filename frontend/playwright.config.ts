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

  // One retry, for one specific failure that is not the application's.
  //
  // Roughly one navigation in sixty, Firefox never completes `page.goto`. The
  // server logs the request served in under a millisecond, the failure
  // report's page snapshot shows the page fully rendered, and the navigation
  // sits there until the test times out. It happens on any page, in no fixed
  // place, over HTTP and HTTPS alike, and waiting for `domcontentloaded`
  // instead of `load` does not avoid it -- the stall is before either event.
  // Chromium has never done it once.
  //
  // A retry is the honest response to that and not a way of hiding a real
  // failure: a test that passes on the second attempt is reported as flaky
  // rather than as passed, so it stays visible, and a genuine defect fails
  // both attempts. The navigation timeout below is what makes the retry cheap
  // -- without it a stall costs the full sixty seconds before the second
  // attempt even starts.
  retries: 1,
  reporter: process.env["CI"] ? "github" : "list",
  // Generous, because these tests are not fast by nature: the live-update one
  // opens two browser contexts, logs both in and seeds a restaurant and an
  // order before it asserts anything. Thirty seconds was enough on a fast
  // machine and not on a two-core VM, which is exactly the kind of flake worth
  // spending thirty more seconds to avoid.
  timeout: 60_000,
  expect: { timeout: 10_000 },

  use: {
    baseURL,
    // A development server uses a self-signed certificate, and refusing it
    // would only test that the certificate is self-signed.
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    // Well under the test timeout, so a stalled navigation gives up and is
    // retried rather than consuming the whole test's budget. No page here
    // takes anything like twenty seconds to arrive: the slowest measured is
    // the order page at well under one.
    navigationTimeout: 20_000,
  },

  // Both browsers docs/12_testing.md names. They are the two this application
  // is designed for, and they disagree about enough -- date inputs, focus
  // handling, EventSource buffering -- to be worth running both.
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
  ],
});
