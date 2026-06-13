import {defineConfig, globalIgnores} from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
    ...nextVitals,
    ...nextTs,
    // Override default ignores of eslint-config-next.
    globalIgnores([
        // Default ignores of eslint-config-next:
        ".next/**",
        "out/**",
        "build/**",
        "next-env.d.ts",
        // Generated release artifacts. `pnpm stage:webui` copies the static export
        // here so Go can embed it; lint the source, not the minified build output.
        "backend/internal/webui/dist/**",
        "playwright-report/**",
        "test-results/**",
    ]),
    {
        files: ["src/**/*.{ts,tsx}", "tests/**/*.{ts,tsx}"],
        rules: {
            // These are existing cleanup debt in the imported codebase. Keep them
            // visible without making the first public CI gate fail before the debt is
            // paid down.
            "@typescript-eslint/no-explicit-any": "warn",
            "react-hooks/set-state-in-effect": "warn",
        },
    },
]);

export default eslintConfig;
