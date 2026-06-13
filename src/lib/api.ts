/**
 * Thin browser client for the Memento Go backend.
 *
 * The UI is a static export served by the Go backend itself, so every call
 * uses a relative path on the same origin (e.g. "/api/people"). In `next dev`
 * the same paths are proxied to the Go backend via the rewrite in
 * `next.config.ts`.
 */
export const API_BASE = "";

/**
 * apiGet fetches `path` (e.g. "/api/people") and returns parsed JSON, or null
 * on any error. Always bypasses the HTTP cache so backend refreshes are
 * visible without a hard reload.
 */
export async function apiGet<T>(path: string): Promise<T | null> {
    try {
        const res = await fetch(`${API_BASE}${path}`, {cache: "no-store"});
        if (!res.ok) {
            console.error(`[memento] GET ${path} -> ${res.status}`);
            return null;
        }
        return (await res.json()) as T;
    } catch (e) {
        console.error(`[memento] GET ${path} failed`, e);
        return null;
    }
}

/**
 * apiPost is the mutating counterpart. Returns parsed JSON or null on
 * error. Used for refresh/generate triggers from the settings panel and
 * dashboard.
 */
export async function apiPost<T>(path: string, body?: unknown): Promise<T | null> {
    try {
        const res = await fetch(`${API_BASE}${path}`, {
            method: "POST",
            headers: body ? {"Content-Type": "application/json"} : undefined,
            body: body ? JSON.stringify(body) : undefined,
            cache: "no-store",
        });
        if (!res.ok) {
            console.error(`[memento] POST ${path} -> ${res.status}`);
            return null;
        }
        return (await res.json()) as T;
    } catch (e) {
        console.error(`[memento] POST ${path} failed`, e);
        return null;
    }
}

/**
 * readCookie returns the value of a cookie by name, or null when absent or
 * when running outside the browser (build-time prerender).
 */
export function readCookie(name: string): string | null {
    if (typeof document === "undefined") return null;
    const match = document.cookie
        .split("; ")
        .find((part) => part.startsWith(`${name}=`));
    return match ? decodeURIComponent(match.slice(name.length + 1)) : null;
}

/**
 * privacyEnabled reads the memento_privacy_enabled cookie (default: true).
 */
export function privacyEnabled(): boolean {
    return readCookie("memento_privacy_enabled") !== "false";
}

/**
 * currentSlug extracts the trailing dynamic segment from the browser URL for
 * statically exported [slug] routes (e.g. "/people/jane/" -> "jane"). The
 * exported shell is built with a placeholder param, so `useParams()` cannot be
 * trusted; the address bar is the source of truth.
 *
 * INVARIANT: this assumes the slug is the LAST path segment. It does not work
 * for nested routes under a slug (e.g. "/people/jane/history" would return
 * "history"). If such a route is ever added, give it its own slug reader and
 * update backend/internal/webui/webui.go (rewriteDynamicSlug) to match.
 */
export function currentSlug(): string {
    if (typeof window === "undefined") return "";
    const parts = window.location.pathname.split("/").filter(Boolean);
    return decodeURIComponent(parts[parts.length - 1] ?? "");
}
