"use client";

import {useCallback, useMemo} from "react";
import {usePathname, useRouter, useSearchParams} from "next/navigation";

export function useAgentRunURLState(flow: string) {
    const router = useRouter();
    const pathname = usePathname();
    const searchParams = useSearchParams();

    const runIdFromURL = useMemo(() => {
        if (searchParams.get("agentFlow") !== flow) return null;
        const raw = searchParams.get("agentRunId");
        if (!raw) return null;
        const parsed = Number.parseInt(raw, 10);
        return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
    }, [flow, searchParams]);

    const replaceParams = useCallback(
        (mutate: (params: URLSearchParams) => void) => {
            const params = new URLSearchParams(searchParams.toString());
            mutate(params);
            const suffix = params.toString();
            router.replace(suffix ? `${pathname}?${suffix}` : pathname, {scroll: false});
        },
        [pathname, router, searchParams],
    );

    const rememberRun = useCallback(
        (runId: number) => {
            replaceParams((params) => {
                params.set("agentFlow", flow);
                params.set("agentRunId", String(runId));
            });
        },
        [flow, replaceParams],
    );

    const clearRun = useCallback(() => {
        replaceParams((params) => {
            if (params.get("agentFlow") === flow) {
                params.delete("agentFlow");
                params.delete("agentRunId");
            }
        });
    }, [flow, replaceParams]);

    return {runIdFromURL, rememberRun, clearRun};
}
