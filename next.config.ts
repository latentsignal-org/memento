import type {NextConfig} from "next";

const allowedDevOrigins = [
    "localhost",
    "127.0.0.1",
    ...(process.env.MEMENTO_ALLOWED_DEV_ORIGINS ?? "")
        .split(",")
        .map((origin) => origin.trim())
        .filter(Boolean),
];

const isDev = process.env.NODE_ENV === "development";

const nextConfig: NextConfig = {
    // Static export: `next build` emits `out/`, which is embedded into the Go
    // binary (backend/internal/webui) and served by `memento serve` / `memento app`.
    //
    // In development (`next dev`) we skip export mode (it forbids rewrites) and
    // instead proxy /api/* to a locally running Go backend so the same relative
    // fetch paths work in both modes.
    ...(isDev
        ? {
            async rewrites() {
                return [
                    {
                        source: "/api/:path*",
                        destination: `${process.env.MEMENTO_BACKEND_URL ?? "http://127.0.0.1:8787"}/api/:path*`,
                    },
                ];
            },
        }
        : {output: "export" as const}),
    trailingSlash: true,
    // Static export has no image optimizer, and the Go static handler does not
    // serve /_next/image. Emit plain <img> src URLs so logos load when the
    // exported UI is embedded in the binary.
    images: {unoptimized: true},
    allowedDevOrigins: Array.from(new Set(allowedDevOrigins)),
};

export default nextConfig;
