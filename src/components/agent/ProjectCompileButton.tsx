"use client";
import {useEffect, useMemo, useRef, useState} from "react";
import {useRouter} from "next/navigation";
import {CheckCircle2, LoaderCircle} from "lucide-react";
import {useAgentStream} from "./useAgentStream";
import {useAgentRunURLState} from "./useAgentRunURLState";
import {getToolLabel} from "@/lib/tool-labels";
import {revalidateEntityPath} from "@/app/actions";
import {computeCompletedSteps, PROJECT_STEPS_CONFIG} from "@/lib/agent-steps";

const SIMULATION_UI_ENABLED = process.env.NEXT_PUBLIC_MEMENTO_AGENT_SIMULATION === "1";

const PROJECT_STEPS = PROJECT_STEPS_CONFIG.steps;

interface ProjectCompileButtonProps {
    slug: string;
    hasGenerated?: boolean;
    onRunningChange?: (running: boolean) => void;
    cardLayout?: "stack" | "full-row";
    simulateByDefault?: boolean;
    simulationDelayMs?: number;
}

export default function ProjectCompileButton({
                                                 slug,
                                                 hasGenerated = true,
                                                 onRunningChange,
                                                 cardLayout = "stack",
                                                 simulateByDefault = false,
                                                 simulationDelayMs,
                                             }: ProjectCompileButtonProps) {
    const router = useRouter();
    const [open, setOpen] = useState(false);
    const resumeStartedRef = useRef(false);
    const {runIdFromURL, rememberRun, clearRun} = useAgentRunURLState("project_compile");
    const {events, isRunning, error, run, resume, reset} = useAgentStream((e) => {
        // done is emitted only after the rollup refresh, so the revalidation here
        // is guaranteed to see the new sections.
        if (e.type === "done") {
            void revalidateEntityPath(`/projects/${slug}`)
                .catch((error) => console.error("revalidate project path", error))
                .finally(clearRun);
        }
    }, rememberRun);

    useEffect(() => {
        if (!runIdFromURL || resumeStartedRef.current || isRunning) return;
        resumeStartedRef.current = true;
        setOpen(true);
        void resume(`/api/agents/runs/${runIdFromURL}/events`);
    }, [isRunning, resume, runIdFromURL]);

    useEffect(() => {
        onRunningChange?.(isRunning);
    }, [isRunning, onRunningChange]);

    const toolCount = events.filter((e) => e.type === "tool_call_start").length;
    const finished = events.some((e) => e.type === "done");
    const lostConnection = !!error && toolCount > 0 && !finished;
    const showCard = open && (lostConnection || isRunning || !!error || (toolCount > 0 && !finished));

    const completedSteps = useMemo(
        () => computeCompletedSteps(PROJECT_STEPS_CONFIG, events),
        [events],
    );

    const currentActivity = useMemo(() => {
        const latest = [...events]
            .reverse()
            .find(
                (e): e is Extract<typeof events[number], { type: "tool_call_start" } | { type: "text_delta" }> =>
                    e.type === "tool_call_start" || e.type === "text_delta",
            );
        if (!latest) return "Preparing project agent";
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
            ? `/api/projects/${slug}/generate?sim=1${Number.isFinite(simulationDelayMs) ? `&sim_delay_ms=${simulationDelayMs}` : ""}`
            : `/api/projects/${slug}/generate`;
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
                        {isRunning ? (
                            <>
                                <LoaderCircle className="h-4 w-4 animate-spin"/>
                                Generating…
                            </>
                        ) : hasGenerated ? "Re-generate" : "Generate with AI"}
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
                        Project brief updated.
                    </div>
                )}
            </div>

            {showCard && (
                <div
                    className={`${fullRowCard ? "col-span-full w-full" : "w-full"} text-xs text-on-surface-variant border border-outline-variant/40 rounded-lg p-4 bg-surface-container-lowest space-y-3`}>
                    {lostConnection ? (
                        <div className="space-y-1.5">
                            <div className="text-amber-700 font-medium">
                                Connection lost — the compile is likely still running on the server.
                            </div>
                            <button
                                className="text-primary underline text-[11px]"
                                onClick={() => window.location.reload()}
                            >
                                Refresh now to check
                            </button>
                        </div>
                    ) : (
                        <>
                            {/* Section progress */}
                            <div className="space-y-2">
                                <div
                                    className="flex items-center justify-between text-[11px] font-medium text-on-surface-variant">
                                    <span>{completedSteps.size}/{PROJECT_STEPS.length} steps</span>
                                    <span className="font-mono">{toolCount} tool call{toolCount === 1 ? "" : "s"}</span>
                                </div>
                                <div className="flex gap-1.5">
                                    {PROJECT_STEPS.map(({key, label}) => {
                                        const done = completedSteps.has(key);
                                        return (
                                            <div
                                                key={key}
                                                className={`flex-1 rounded px-1.5 py-1 text-center text-[10px] font-semibold transition-colors ${
                                                    done
                                                        ? "bg-primary/15 text-primary"
                                                        : "bg-surface-container-low text-on-surface-variant/50"
                                                }`}
                                            >
                                                {done ? "✓ " : ""}{label}
                                            </div>
                                        );
                                    })}
                                </div>
                                {/* Progress bar */}
                                <div className="h-1 rounded-full bg-outline-variant/30 overflow-hidden">
                                    <div
                                        className="h-full rounded-full bg-primary transition-all duration-500"
                                        style={{width: `${(completedSteps.size / PROJECT_STEPS.length) * 100}%`}}
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
