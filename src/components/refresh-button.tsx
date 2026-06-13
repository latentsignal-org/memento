"use client";

import {useEffect, useMemo, useRef, useState} from "react";
import {CheckCircle2, LoaderCircle} from "lucide-react";

interface JobEvent {
    timestamp: string;
    message: string;
    status: "pending" | "running" | "succeeded" | "failed" | "";
}

interface JobSnapshot {
    status: JobEvent["status"];
    error?: string;
    events?: JobEvent[];
}

interface ProgressStep {
    key: string;
    label: string;
    patterns: string[];
}

interface Props {
    label: string;
    runningLabel?: string;
    endpoint: string; // e.g. "/api/people/refresh"
    apiBase?: string; // override of the Go API base
    triggerApiBase?: string; // override only for the initial POST that starts the job
    onSucceeded?: () => void | Promise<void>;
    className?: string;
    statusVariant?: "inline" | "card";
    panelWidthClassName?: string;
    cardLayout?: "stack" | "full-row";
    onRunningChange?: (running: boolean) => void;
    progressSteps?: ProgressStep[];
    successMessage?: string;
    simulateByDefault?: boolean;
    simulationDelayMs?: number;
    simulationMessages?: string[];
}

/**
 * RefreshButton triggers a background refresh job on the Go API and streams
 * progress events from /api/jobs/:id via SSE until the job terminates.
 * Keeps the default UI minimal; card mode can opt into richer step progress.
 */
export default function RefreshButton({
                                          label,
                                          runningLabel = "In progress…",
                                          endpoint,
                                          apiBase = "",
                                          triggerApiBase,
                                          onSucceeded,
                                          className,
                                          statusVariant = "inline",
                                          panelWidthClassName = "w-full",
                                          cardLayout = "stack",
                                          onRunningChange,
                                          progressSteps,
                                          successMessage = "Updated.",
                                          simulateByDefault = false,
                                          simulationDelayMs = 700,
                                          simulationMessages,
                                      }: Props) {
    const [status, setStatus] = useState<JobEvent["status"]>("");
    const [message, setMessage] = useState<string>("");
    const [messages, setMessages] = useState<string[]>([]);
    const sourceRef = useRef<EventSource | null>(null);
    const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
    const finalizedRef = useRef(false);

    useEffect(() => {
        return () => {
            sourceRef.current?.close();
            if (pollRef.current) clearInterval(pollRef.current);
        };
    }, []);

    const stopWatchingJob = () => {
        sourceRef.current?.close();
        sourceRef.current = null;
        if (pollRef.current) {
            clearInterval(pollRef.current);
            pollRef.current = null;
        }
    };

    const finalizeSucceeded = () => {
        if (finalizedRef.current) return;
        finalizedRef.current = true;
        setStatus("running");
        setMessage("Updating page…");
        setMessages((prev) => [...prev, "Updating page…"]);
        setStatus("succeeded");
        stopWatchingJob();
        void (async () => {
            try {
                await onSucceeded?.();
            } catch (error) {
                const errorMessage = error instanceof Error ? error.message : String(error);
                console.error("Refresh after successful job failed:", errorMessage);
            }
        })();
    };

    const finalizeFailed = (errorMessage?: string) => {
        if (finalizedRef.current) return;
        finalizedRef.current = true;
        setStatus("failed");
        if (errorMessage) {
            setMessage(errorMessage);
            setMessages((prev) => [...prev, errorMessage]);
        }
        stopWatchingJob();
    };

    const observeTerminalStatus = (event: Pick<JobEvent, "status" | "message">) => {
        if (event.status === "succeeded") {
            finalizeSucceeded();
            return true;
        }
        if (event.status === "failed") {
            finalizeFailed(event.message || "Job failed");
            return true;
        }
        return false;
    };

    const run = async () => {
        stopWatchingJob();
        finalizedRef.current = false;
        setStatus("pending");
        setMessage("Starting…");
        setMessages(["Starting…"]);
        if (simulateByDefault) {
            const cadence = Math.max(0, Math.floor(simulationDelayMs));
            const simulatedMessages = simulationMessages?.length
                ? simulationMessages
                : ["Starting…", "Running simulated job…", "Saved simulated output."];
            setStatus("running");
            for (const nextMessage of simulatedMessages) {
                setMessage(nextMessage);
                setMessages((prev) => [...prev, nextMessage]);
                await new Promise((resolve) => setTimeout(resolve, cadence));
            }
            setStatus("succeeded");
            onSucceeded?.();
            return;
        }
        try {
            const postBase = triggerApiBase ?? apiBase;
            const res = await fetch(`${postBase}${endpoint}`, {method: "POST"});
            if (!res.ok) {
                setStatus("failed");
                setMessage(`HTTP ${res.status}`);
                setMessages((prev) => [...prev, `HTTP ${res.status}`]);
                return;
            }
            const {job_id} = await res.json();
            const es = new EventSource(`${apiBase}/api/jobs/${job_id}`);
            sourceRef.current = es;
            setStatus("running");
            pollRef.current = setInterval(() => {
                void (async () => {
                    if (finalizedRef.current) return;
                    try {
                        const statusRes = await fetch(`${apiBase}/api/jobs/${job_id}/status`, {cache: "no-store"});
                        if (!statusRes.ok) return;
                        const snapshot = (await statusRes.json()) as JobSnapshot;
                        const latest = snapshot.events?.at(-1);
                        if (latest?.message) {
                            setMessage(latest.message);
                            setMessages((prev) => prev.at(-1) === latest.message ? prev : [...prev, latest.message]);
                        }
                        observeTerminalStatus({
                            status: snapshot.status,
                            message: snapshot.error || latest?.message || "",
                        });
                    } catch {
                        // SSE remains the primary channel; polling failures are non-terminal.
                    }
                })();
            }, 2000);
            es.onmessage = async (ev) => {
                try {
                    const event = JSON.parse(ev.data) as JobEvent;
                    const nextMessage = event.message || "";
                    if (nextMessage) {
                        setMessage(nextMessage);
                        setMessages((prev) => [...prev, nextMessage]);
                    }

                    if (observeTerminalStatus(event)) return;

                    if (event.status) setStatus(event.status);
                } catch (error) {
                    const errorMessage = error instanceof Error ? error.message : String(error);
                    setStatus("failed");
                    setMessage(errorMessage);
                    setMessages((prev) => [...prev, errorMessage]);
                    es.close();
                    sourceRef.current = null;
                }
            };
            es.onerror = () => {
                es.close();
                sourceRef.current = null;
                if (!finalizedRef.current) {
                    setMessage("Waiting for job status…");
                    setMessages((prev) => [...prev, "Waiting for job status…"]);
                }
            };
        } catch (e) {
            const errorMessage = e instanceof Error ? e.message : String(e);
            setStatus("failed");
            setMessage(errorMessage);
            setMessages((prev) => [...prev, errorMessage]);
        }
    };

    const running = status === "pending" || status === "running";
    const succeeded = status === "succeeded";
    const failed = status === "failed";
    const richProgress = statusVariant === "card" && progressSteps && progressSteps.length > 0;
    const showCard = statusVariant === "card" && !succeeded && (message || running || failed);

    const completedSteps = useMemo(() => {
        const normalizedMessages = messages.map((item) => item.toLowerCase());
        const steps = new Set<string>();
        if (!progressSteps) return steps;
        for (const step of progressSteps) {
            if (succeeded || normalizedMessages.some((item) => step.patterns.some((pattern) => item.includes(pattern.toLowerCase())))) {
                steps.add(step.key);
            }
        }
        return steps;
    }, [messages, progressSteps, succeeded]);

    const buttonClass = running
        ? "inline-flex items-center gap-2 rounded bg-surface-container-high px-4 py-2 text-sm font-semibold text-on-surface-variant shadow-sm ring-1 ring-outline-variant/60"
        : "inline-flex items-center gap-2 rounded bg-primary px-4 py-2 text-sm font-semibold text-white shadow-sm hover:opacity-90 disabled:opacity-50";

    useEffect(() => {
        onRunningChange?.(running);
    }, [running, onRunningChange]);

    const fullRowCard = statusVariant === "card" && cardLayout === "full-row";

    return (
        <div
            className={fullRowCard ? "contents" : `${statusVariant === "card" ? `flex ${showCard ? panelWidthClassName : "w-auto"} max-w-full flex-col items-end space-y-2` : "space-y-1.5"}`}>
            <div className={`${fullRowCard ? "justify-self-end" : ""} flex flex-col items-end gap-1.5`}>
                <button
                    type="button"
                    onClick={run}
                    disabled={running}
                    className={`${buttonClass} ${running ? "cursor-wait" : "cursor-pointer"} ${className ?? ""}`}
                >
                    {running ? <LoaderCircle className="h-4 w-4 animate-spin"/> : null}
                    {running ? runningLabel : label}
                </button>
                {succeeded && statusVariant === "card" ? (
                    <div className="flex items-center gap-1.5 text-[11px] font-medium text-primary">
                        <CheckCircle2 className="h-3.5 w-3.5"/>
                        {successMessage}
                    </div>
                ) : null}
            </div>
            {message && statusVariant === "inline" ? (
                <p className={`text-[10px] leading-relaxed pl-1 ${failed ? "text-destructive" : "text-on-surface-variant"}`}>
                    {message}
                </p>
            ) : null}
            {showCard ? (
                <div
                    className={`${fullRowCard ? "col-span-full w-full" : `${panelWidthClassName} self-stretch`} text-xs text-on-surface-variant border border-outline-variant/40 rounded-lg p-4 bg-surface-container-lowest space-y-3`}>
                    {richProgress ? (
                        <>
                            <div className="space-y-2">
                                <div
                                    className="flex items-center justify-between text-[11px] font-medium text-on-surface-variant">
                                    <span>{completedSteps.size}/{progressSteps.length} steps</span>
                                    <span className="font-mono">{failed ? "failed" : "running"}</span>
                                </div>
                                <div className="flex gap-1.5">
                                    {progressSteps.map(({key, label: stepLabel}) => {
                                        const done = completedSteps.has(key);
                                        return (
                                            <div
                                                key={key}
                                                className={`flex-1 rounded px-1.5 py-1 text-center text-[10px] font-semibold transition-colors ${
                                                    done ? "bg-primary/15 text-primary" : "bg-surface-container-low text-on-surface-variant/50"
                                                }`}
                                            >
                                                {done ? "✓ " : ""}{stepLabel}
                                            </div>
                                        );
                                    })}
                                </div>
                                <div className="h-1 rounded-full bg-outline-variant/30 overflow-hidden">
                                    <div
                                        className="h-full rounded-full bg-primary transition-all duration-500"
                                        style={{width: `${(completedSteps.size / progressSteps.length) * 100}%`}}
                                    />
                                </div>
                            </div>
                            <div
                                className={`truncate border-t border-outline-variant/30 pt-2 font-mono text-[11px] ${failed ? "text-red-600" : "text-on-surface-variant/75"}`}>
                                → {message || "Preparing newsletter generator"}
                            </div>
                        </>
                    ) : (
                        <div className={failed ? "text-red-600" : "text-on-surface-variant"}>{message}</div>
                    )}
                </div>
            ) : null}
        </div>
    );
}
