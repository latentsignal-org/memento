/**
 * Helpers for statically exported pages that read their dynamic context
 * (slug, query flags) from the browser URL instead of server props.
 */

export interface SimulationFlags {
    simulationMode: boolean;
    simulationDelayMs: number | null;
}

/** Parse ?sim=1&sim_delay_ms=NNN from the current URL (debug/demo flags). */
export function simulationFromLocation(): SimulationFlags {
    if (typeof window === "undefined") {
        return {simulationMode: false, simulationDelayMs: null};
    }
    const params = new URLSearchParams(window.location.search);
    const simulationMode = params.get("sim") === "1";
    const rawDelay = params.get("sim_delay_ms");
    const parsed = rawDelay ? Number(rawDelay) : NaN;
    const simulationDelayMs =
        Number.isFinite(parsed) && parsed >= 0 ? Math.floor(parsed) : null;
    return {simulationMode, simulationDelayMs};
}

/**
 * The exported [slug] shells are built with this placeholder param; the Go
 * server rewrites real slug URLs onto them and the client reads the real
 * slug from `window.location` (see `currentSlug` in lib/api).
 */
export const STATIC_SLUG_PLACEHOLDER = "_";

export function staticSlugParams(): Array<{ slug: string }> {
    return [{slug: STATIC_SLUG_PLACEHOLDER}];
}
