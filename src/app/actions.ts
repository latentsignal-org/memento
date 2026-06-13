"use client";

/**
 * Compatibility shim for the former Next.js server action.
 *
 * The UI is now a static export: there is no server-side render cache to
 * revalidate. Pages fetch their data client-side on every mount, so a
 * revalidation request is a no-op; callers that need fresh data on the
 * current page trigger `window.location.reload()` instead.
 */
export async function revalidateEntityPath(_path: string): Promise<void> {
    // Intentionally empty — client pages always fetch fresh data on mount.
}
