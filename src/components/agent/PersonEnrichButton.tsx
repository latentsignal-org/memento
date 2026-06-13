"use client";
import {useCallback, useEffect, useMemo, useRef, useState} from "react";
import {useRouter} from "next/navigation";
import {CheckCircle2, LoaderCircle, UserRoundCog} from "lucide-react";
import {useAgentStream} from "./useAgentStream";
import {useAgentRunURLState} from "./useAgentRunURLState";
import {getToolLabel} from "@/lib/tool-labels";
import {revalidateEntityPath} from "@/app/actions";
import {computeCompletedSteps, PERSON_STEPS_CONFIG} from "@/lib/agent-steps";

const SIMULATION_UI_ENABLED = process.env.NEXT_PUBLIC_MEMENTO_AGENT_SIMULATION === "1";

const PERSON_STEPS = PERSON_STEPS_CONFIG.steps;

interface PersonEnrichButtonProps {
    slug: string;
    hasGenerated?: boolean;
    onRunningChange?: (running: boolean) => void;
    cardLayout?: "stack" | "full-row";
    simulateByDefault?: boolean;
    simulationDelayMs?: number;
}

export default function PersonEnrichButton({
                                               slug,
                                               hasGenerated = true,
                                               onRunningChange,
                                               cardLayout = "stack",
                                               simulateByDefault = false,
                                               simulationDelayMs,
                                           }: PersonEnrichButtonProps) {
    const router = useRouter();
    const [open, setOpen] = useState(false);
    const resumeStartedRef = useRef(false);
    const [resumePolling, setResumePolling] = useState(false);
    const [resumeStatus, setResumeStatus] = useState<"running" | "succeeded" | "failed" | null>(null);
    const {runIdFromURL, rememberRun, clearRun} = useAgentRunURLState("person_enrich");
    const {events, isRunning, error, run, resume: _resume, reset} = useAgentStream((e) => {
        if (e.type === "done") {
            void revalidateEntityPath(`/people/${slug}`)
                .catch((error) => console.error("revalidate people path", error))
                .finally(clearRun);
        }
    }, rememberRun);

    // When navigating back to the page mid-generation, the component re-mounts
    // with no SSE connection. Check session status immediately, then poll every
    // 4s if still active. clearRun() navigates to the clean URL (no params)
    // which for force-dynamic pages re-fetches fresh server data — no separate
    // window.location.reload() needed.
    const checkAndHandleSession = useCallback(
        async (sessionId: number, cancelled: () => boolean) => {
            while (!cancelled()) {
                try {
                    const res = await fetch(`/api/agents/sessions/${sessionId}`);
                    if (!res.ok) {
                        clearRun();
                        setResumePolling(false);
                        setResumeStatus(null);
                        return;
                    }
                    const session = (await res.json()) as { status: string };
                    if (session.status === "succeeded") {
                        setResumeStatus("succeeded");
                        setResumePolling(false);
                        clearRun(); // router.replace to clean URL → re-fetches fresh data
                        return;
                    }
                    if (session.status === "failed") {
                        setResumeStatus("failed");
                        setResumePolling(false);
                        clearRun();
                        return;
                    }
                    // Still active — wait 4s then check again
                    await new Promise((r) => setTimeout(r, 4000));
                } catch {
                    // Network blip — wait and retry
                    await new Promise((r) => setTimeout(r, 4000));
                }
            }
        },
        [clearRun],
    );

    useEffect(() => {
        if (!runIdFromURL || resumeStartedRef.current || isRunning) return;
        resumeStartedRef.current = true;
        setOpen(true);
        setResumePolling(true);
        setResumeStatus("running");
        let isCancelled = false;
        void checkAndHandleSession(runIdFromURL, () => isCancelled);
        return () => {
            isCancelled = true;
        };
    }, [isRunning, runIdFromURL, checkAndHandleSession]);

    const toolCount = events.filter((e) => e.type === "tool_call_start").length;
    const finished = events.some((e) => e.type === "done");
    const lostConnection = !!error && toolCount > 0 && !finished;
    const showCard = open && (resumePolling || lostConnection || isRunning || !!error || (toolCount > 0 && !finished));

    useEffect(() => {
        onRunningChange?.(isRunning);
    }, [isRunning, onRunningChange]);

    const completedSteps = useMemo(
        () => computeCompletedSteps(PERSON_STEPS_CONFIG, events),
        [events],
    );

    const currentActivity = useMemo(() => {
        const latest = [...events]
            .reverse()
            .find(
                (e): e is Extract<typeof events[number], { type: "tool_call_start" } | { type: "text_delta" }> =>
                    e.type === "tool_call_start" || e.type === "text_delta",
            );
        if (!latest) return "Preparing relationship agent";
        if (latest.type === "text_delta") return latest.text.replace(/\s+/g, " ").trim();
        return getToolLabel(latest.name, latest.args);
    }, [events]);

    const start = async (simulate = false) => {
        setOpen(true);
        reset();
        clearRun();
        resumeStartedRef.current = false;
        const shouldSimulate = simulate || simulateByDefault;
        const endpoint = shouldSimulate
            ? `/api/people/${slug}/enrich?sim=1${Number.isFinite(simulationDelayMs) ? `&sim_delay_ms=${simulationDelayMs}` : ""}`
            : `/api/people/${slug}/enrich`;
        await run(endpoint, {});
    };

    const buttonClass = isRunning
        ? "inline-flex items-center gap-2 rounded bg-surface-container-high px-4 py-2 text-sm font-semibold text-on-surface-variant shadow-sm ring-1 ring-outline-variant/60"
        : "inline-flex items-center gap-2 rounded bg-primary px-4 py-2 text-sm font-semibold text-white shadow-sm hover:opacity-90 disabled:opacity-50";

    const fullRowCard = cardLayout === "full-row";

    return (
        <div
            className={fullRowCard ? "contents" : `flex max-w-full flex-col items-end space-y-2 ${showCard ? "w-full" : "w-auto"}`}>
            <div className={`${fullRowCard ? "justify-self-end" : ""} flex flex-col items-end gap-1.5`}>
                <div className="flex flex-wrap items-center justify-end gap-2">
                    <button className={buttonClass} onClick={() => start(false)} disabled={isRunning}>
                        {isRunning ? <LoaderCircle className="h-4 w-4 animate-spin"/> :
                            <UserRoundCog className="h-4 w-4"/>}
                        {isRunning ? "Generating…" : hasGenerated ? "Re-generate" : "Generate with AI"}
                    </button>
                    {SIMULATION_UI_ENABLED && (
                        <button
                            type="button"
                            className="inline-flex items-center gap-2 rounded border border-outline-variant/70 bg-surface-container px-3 py-2 text-xs font-semibold text-on-surface hover:bg-surface-container-high disabled:opacity-50"
                            onClick={() => start(true)}
                            disabled={isRunning}
                        >
                            Simulate
                        </button>
                    )}
                </div>
                {finished && !isRunning && !error && (
                    <div className="flex items-center gap-1.5 text-[11px] font-medium text-primary">
                        <CheckCircle2 className="h-3.5 w-3.5"/>
                        Relationship brief updated.
                    </div>
                )}
            </div>

            {showCard && (
                <div
                    className={`${fullRowCard ? "col-span-full w-full" : "w-full self-stretch"} text-xs text-on-surface-variant border border-outline-variant/40 rounded-lg p-4 bg-surface-container-lowest space-y-3`}>
                    {resumePolling ? (
                        <div className="space-y-2">
                            {resumeStatus === "failed" ? (
                                <div className="text-red-600 font-medium">
                                    Generation failed on the server. Please try again.
                                </div>
                            ) : resumeStatus === "succeeded" ? (
                                <div className="flex items-center gap-1.5 text-primary font-medium">
                                    <CheckCircle2 className="h-3.5 w-3.5"/>
                                    Brief generated — refreshing page…
                                </div>
                            ) : (
                                <div className="flex items-center gap-2 text-on-surface-variant font-medium">
                                    <LoaderCircle className="h-3.5 w-3.5 animate-spin shrink-0"/>
                                    Generation in progress — checking status every few seconds…
                                </div>
                            )}
                            {resumeStatus === "running" && (
                                <button className="text-primary underline text-[11px]"
                                        onClick={() => window.location.reload()}>
                                    Refresh now
                                </button>
                            )}
                        </div>
                    ) : lostConnection ? (
                        <div className="space-y-1.5">
                            <div className="text-amber-700 font-medium">
                                Connection lost — the enrichment is likely still running on the server.
                            </div>
                            <button className="text-primary underline text-[11px]"
                                    onClick={() => window.location.reload()}>
                                Refresh now to check
                            </button>
                        </div>
                    ) : (
                        <>
                            <div className="space-y-2">
                                <div
                                    className="flex items-center justify-between text-[11px] font-medium text-on-surface-variant">
                                    <span>{completedSteps.size}/{PERSON_STEPS.length} steps</span>
                                    <span className="font-mono">{toolCount} tool call{toolCount === 1 ? "" : "s"}</span>
                                </div>
                                <div className="flex gap-1.5">
                                    {PERSON_STEPS.map(({key, label}) => {
                                        const done = completedSteps.has(key);
                                        return (
                                            <div
                                                key={key}
                                                className={`flex-1 rounded px-1.5 py-1 text-center text-[10px] font-semibold transition-colors ${
                                                    done ? "bg-primary/15 text-primary" : "bg-surface-container-low text-on-surface-variant/50"
                                                }`}
                                            >
                                                {done ? "✓ " : ""}{label}
                                            </div>
                                        );
                                    })}
                                </div>
                                <div className="h-1 rounded-full bg-outline-variant/30 overflow-hidden">
                                    <div
                                        className="h-full rounded-full bg-primary transition-all duration-500"
                                        style={{width: `${(completedSteps.size / PERSON_STEPS.length) * 100}%`}}
                                    />
                                </div>
                            </div>
                            <div
                                className="truncate border-t border-outline-variant/30 pt-2 font-mono text-[11px] text-on-surface-variant/75">
                                → {currentActivity}
                            </div>
                        </>
                    )}
                    {error && !lostConnection && <div className="text-red-600">Error: {error}</div>}
                </div>
            )}
        </div>
    );
}
