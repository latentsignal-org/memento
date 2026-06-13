"use client";

import {useCallback, useEffect, useMemo, useRef, useState} from "react";
import {ChevronDown, Clock, LoaderCircle, RefreshCw, Terminal, Wrench} from "lucide-react";
import {safePreviewJson, safePreviewText} from "./panel-safety";
import {getToolLabel} from "@/lib/tool-labels";
import {normalizeLogsResponse} from "@/lib/agent-logs";

interface AgentLoopLog {
    step_index: number;
    input_type: string;
    input_content: string;
    assistant_text: string;
    reasoning_text: string;
    tool_calls_json: string;
    tool_results_json: string;
    duration_ms: number;
    created_at: string;
}

interface Props {
    sessionType: "project_compile" | "concept_compile" | "person_enrich";
    entityId: string;
    buttonStyle?: "default" | "link";
}

export default function ExecutionTracePanel({sessionType, entityId, buttonStyle = "default"}: Props) {
    const [open, setOpen] = useState(false);
    const [logs, setLogs] = useState<AgentLoopLog[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const rootRef = useRef<HTMLDivElement | null>(null);

    const fetchLogs = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const res = await fetch(`/api/agents/logs?type=${sessionType}&entityId=${entityId}`);
            if (!res.ok) throw new Error("Unable to load trace.");
            setLogs(normalizeLogsResponse<AgentLoopLog>(await res.json()).loops);
        } catch (e) {
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            setLoading(false);
        }
    }, [entityId, sessionType]);

    useEffect(() => {
        if (open) fetchLogs();
    }, [open, fetchLogs]);

    useEffect(() => {
        if (!open) return;

        const onPointerDown = (event: MouseEvent) => {
            if (!rootRef.current?.contains(event.target as Node)) {
                setOpen(false);
            }
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                setOpen(false);
            }
        };

        document.addEventListener("mousedown", onPointerDown);
        document.addEventListener("keydown", onKeyDown);
        return () => {
            document.removeEventListener("mousedown", onPointerDown);
            document.removeEventListener("keydown", onKeyDown);
        };
    }, [open]);

    return (
        <div ref={rootRef} className="relative inline-flex justify-end">
            <div className="flex items-center justify-end gap-2">
                <button
                    type="button"
                    onClick={() => setOpen((v) => !v)}
                    className={
                        buttonStyle === "link"
                            ? `inline-flex items-center gap-1.5 text-[12px] font-semibold transition ${
                                open ? "text-primary" : "text-on-surface-variant hover:text-primary"
                            }`
                            : `inline-flex items-center gap-1.5 rounded border px-2.5 py-1 text-[11px] font-semibold transition ${
                                open
                                    ? "border-primary bg-primary text-white"
                                    : "border-outline-variant/60 bg-background text-on-surface-variant hover:bg-surface-container"
                            }`
                    }
                >
                    <Terminal className="h-3 w-3"/>
                    {open ? "Hide Trace" : "Show Trace"}
                </button>
                {open && (
                    <button onClick={fetchLogs}
                            className="p-1 rounded hover:bg-outline-variant/30 text-on-surface-variant"
                            title="Refresh trace">
                        <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`}/>
                    </button>
                )}
            </div>
            {open && (
                <div
                    className="absolute right-0 top-full z-30 mt-2 w-[360px] max-w-[min(360px,calc(100vw-2rem))] max-h-72 overflow-y-auto rounded border border-outline-variant/40 bg-surface-container-low p-3 space-y-3 text-xs shadow-lg">
                    {loading && logs.length === 0 ? (
                        <div className="text-on-surface-variant flex items-center gap-2"><LoaderCircle
                            className="h-4 w-4 animate-spin"/>Loading trace...</div>
                    ) : error ? (
                        <div className="text-red-600">{error}</div>
                    ) : logs.length === 0 ? (
                        <div className="text-on-surface-variant">No trace available for this run.</div>
                    ) : (
                        logs.map((log) => <LogStepCard key={log.step_index} log={log}/>)
                    )}
                </div>
            )}
        </div>
    );
}

function LogStepCard({log}: { log: AgentLoopLog }) {
    const toolCalls = useMemo(() => {
        try {
            // A step with no tool calls is serialized as the literal "null"
            // (Go marshals a nil slice that way), so guard against non-arrays.
            const parsed = JSON.parse(log.tool_calls_json || "[]");
            return Array.isArray(parsed) ? parsed : [];
        } catch {
            return [];
        }
    }, [log.tool_calls_json]);
    const toolResults = useMemo(() => {
        try {
            const parsed = JSON.parse(log.tool_results_json || "[]");
            return Array.isArray(parsed) ? parsed : [];
        } catch {
            return [];
        }
    }, [log.tool_results_json]);
    const [showReasoning, setShowReasoning] = useState(false);
    const [showPayloads, setShowPayloads] = useState(false);
    return (
        <div className="rounded border border-outline-variant/40 bg-surface-container-lowest p-2.5 space-y-2">
            <div className="flex items-center justify-between">
                <span
                    className="font-semibold text-on-surface bg-primary/10 text-primary px-1.5 py-0.5 rounded text-[10px]">Step {log.step_index}</span>
                {log.duration_ms ? <span className="text-[10px] text-on-surface-variant flex items-center gap-1"><Clock
                    className="h-3 w-3"/>{(log.duration_ms / 1000).toFixed(1)}s</span> : null}
            </div>
            {log.input_content ? (
                <div className="space-y-1">
                    <div className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Input
                    </div>
                    <div
                        className="rounded bg-surface-container-low px-2 py-1 text-[11px] text-on-surface-variant leading-relaxed">
                        {summarizeInput(log.input_content)}
                    </div>
                </div>
            ) : null}
            {toolCalls.length > 0 && (
                <div>
                    <div
                        className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider flex items-center gap-1">
                        <Wrench className="h-3 w-3"/>Tools
                    </div>
                    <div className="mt-1 flex flex-wrap gap-1">
                        {toolCalls.map((tc: any, i: number) => (
                            <span key={i}
                                  className="rounded bg-surface-container-low px-1.5 py-0.5 font-mono text-[9px] border border-outline-variant/20 text-on-surface">
                {getToolLabel(tc.name, tc.args)}
              </span>
                        ))}
                    </div>
                </div>
            )}
            {(toolCalls.length > 0 || toolResults.length > 0) && (
                <button
                    type="button"
                    onClick={() => setShowPayloads((v) => !v)}
                    className="inline-flex items-center gap-1 text-[10px] font-semibold text-primary hover:underline"
                >
                    <ChevronDown className={`h-3 w-3 transition-transform ${showPayloads ? "rotate-180" : ""}`}/>
                    {showPayloads ? "Hide args/results" : "Show args/results"}
                </button>
            )}
            {showPayloads && toolCalls.length > 0 ? (
                <div className="space-y-2">
                    {toolCalls.map((tc: any, i: number) => (
                        <div key={`${tc.name}-${i}`}
                             className="rounded border border-outline-variant/30 bg-surface-container-low p-2">
                            <div
                                className="text-[10px] font-semibold text-on-surface">{getToolLabel(tc.name, tc.args)}</div>
                            <div className="mt-1 text-[10px] text-on-surface-variant leading-relaxed break-words">
                                args: {summarizeJson(tc.args)}
                            </div>
                            {toolResults[i] ? (
                                <div className="mt-1 text-[10px] text-on-surface-variant leading-relaxed break-words">
                                    result: {summarizeJson(toolResults[i].result)}
                                </div>
                            ) : null}
                        </div>
                    ))}
                </div>
            ) : null}
            {log.assistant_text ? <div
                className="text-on-surface whitespace-pre-wrap leading-relaxed border-l-2 border-primary/20 pl-2 italic">{log.assistant_text}</div> : null}
            {log.reasoning_text ? (
                <div className="space-y-1">
                    <button
                        type="button"
                        onClick={() => setShowReasoning((v) => !v)}
                        className="inline-flex items-center gap-1 text-[10px] font-semibold text-primary hover:underline"
                    >
                        <ChevronDown className={`h-3 w-3 transition-transform ${showReasoning ? "rotate-180" : ""}`}/>
                        {showReasoning ? "Hide reasoning" : "Show reasoning"}
                    </button>
                    {showReasoning ? (
                        <div
                            className="text-[10px] leading-relaxed text-on-surface-variant bg-surface-container-low/60 p-2 rounded border border-outline-variant/35 font-mono whitespace-pre-wrap break-words">
                            {log.reasoning_text}
                        </div>
                    ) : null}
                </div>
            ) : null}
        </div>
    );
}

function summarizeInput(input: string): string {
    return safePreviewText(input, 180);
}

function summarizeJson(value: unknown): string {
    return safePreviewJson(value, 220);
}
