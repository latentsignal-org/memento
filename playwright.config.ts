import {defineConfig, devices} from "@playwright/test";

// The E2E suite requires a pre-built binary with the UI embedded.
// Run `pnpm package` once before `pnpm e2e`, then keep the binary around
// for subsequent runs (reuseExistingServer is true outside CI).
//
// Seed the fixture database:   pnpm e2e:seed
// Run the suite:               pnpm e2e

const E2E_DB = process.env.MEMENTO_E2E_DB ?? "/tmp/memento-e2e.sqlite";
const BACKEND_PORT = 18787;
const BASE_URL = `http://127.0.0.1:${BACKEND_PORT}`;

const e2eEnv = {
    ...process.env,
    MEMENTO_MSGVAULT_DB: E2E_DB,
    MEMENTO_SKIP_DOTENV: "1",
    MEMENTO_MODEL_PROVIDER: "fake",
    MEMENTO_AGENT_MODEL: "fake",
    GOCACHE: "/tmp/memento-go-build",
};

export default defineConfig({
    testDir: "./tests/e2e",
    timeout: 30_000,
    expect: {
        timeout: 10_000,
    },
    fullyParallel: false,
    reporter: [["list"], ["html", {open: "never"}]],
    use: {
        baseURL: BASE_URL,
        trace: "on-first-retry",
    },
    webServer: [
        {
            // Requires a built binary (`pnpm package`). The binary embeds the UI
            // and serves both the API and static pages on one origin.
            command: `./memento serve --db ${E2E_DB} --port ${BACKEND_PORT}`,
            url: `${BASE_URL}/api/health`,
            reuseExistingServer: !process.env.CI,
            timeout: 30_000,
            env: e2eEnv,
        },
    ],
    projects: [
        {
            name: "chromium",
            use: {...devices["Desktop Chrome"]},
        },
    ],
});
