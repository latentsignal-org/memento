"use client";
import {useCallback, useEffect, useRef, useState} from "react";
import type {AgentEvent} from "@/lib/agent-events";

export type {AgentEvent} from "@/lib/agent-events";

export interface UseAgentStreamResult {
    events: AgentEvent[];
    isRunning: boolean;
    error: string | null;
    runId: number | null;
    run: (url: string, body: unknown) => Promise<void>;
    resume: (url: string) => Promise<void>;
    reset: () => void;
}

export function useAgentStream(
    onEvent?: (e: AgentEvent) => void,
    onRunStart?: (runId: number) => void,
    onResponseHeaders?: (headers: Headers) => void,
): UseAgentStreamResult {
    const [events, setEvents] = useState<AgentEvent[]>([]);
    const [isRunning, setIsRunning] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [runId, setRunId] = useState<number | null>(null);
    const abortRef = useRef<AbortController | null>(null);
    const sawDoneRef = useRef(false);
    const currentRunIdRef = useRef<number | null>(null);

    const checkSessionOnMissingDone = useCallback(async () => {
        const rid = currentRunIdRef.current;
        if (!rid || sawDoneRef.current) return;
        try {
            const res = await fetch(`/api/agents/sessions/${rid}`);
            if (!res.ok) return;
            const session = (await res.json()) as { status: string };
            if (session.status === "succeeded") {
                const doneEvent: AgentEvent = {type: "done", interaction_id: String(rid)};
                setEvents((prev) => [...prev, doneEvent]);
                onEvent?.(doneEvent);
            }
        } catch {
            // best-effort
        }
    }, [onEvent]);

    const consume = useCallback(
        async (res: Response) => {
            sawDoneRef.current = false;
            onResponseHeaders?.(res.headers);
            const headerRunId = res.headers.get("X-Memento-Agent-Run-ID");
            if (headerRunId) {
                const parsed = Number.parseInt(headerRunId, 10);
                if (Number.isFinite(parsed) && parsed > 0) {
                    setRunId(parsed);
                    currentRunIdRef.current = parsed;
                    onRunStart?.(parsed);
                }
            }
            if (!res.ok || !res.body) {
                throw new Error(`HTTP ${res.status}`);
            }
            const reader = res.body.getReader();
            const decoder = new TextDecoder();
            let buf = "";
            while (true) {
                const {value, done} = await reader.read();
                if (done) break;
                buf += decoder.decode(value, {stream: true});

                // Split on SSE message boundary (blank line). Each message is
                // one or more `data: <json>` lines we joined as a single payload.
                let bidx;
                while ((bidx = buf.indexOf("\n\n")) !== -1) {
                    const raw = buf.slice(0, bidx);
                    buf = buf.slice(bidx + 2);
                    const dataLine = raw
                        .split("\n")
                        .filter((l) => l.startsWith("data: "))
                        .map((l) => l.slice(6))
                        .join("");
                    if (!dataLine) continue;
                    try {
                        const ev = JSON.parse(dataLine) as AgentEvent;
                        setEvents((prev) => [...prev, ev]);
                        if (ev.type === "done") sawDoneRef.current = true;
                        if (ev.type === "error") {
                            console.error("agent stream error event", ev.message);
                            setError(ev.message);
                        }
                        onEvent?.(ev);
                    } catch (e) {
                        console.warn("bad SSE chunk", dataLine, e);
                    }
                }
            }
            if (buf.trim()) {
                console.warn("unterminated SSE buffer", buf);
            }
            // Defensive: if the SSE stream ended without a done event, check session
            void checkSessionOnMissingDone();
        },
        [onEvent, onRunStart, onResponseHeaders, checkSessionOnMissingDone],
    );

    const run = useCallback(
        async (url: string, body: unknown) => {
            setEvents([]);
            setIsRunning(true);
            setError(null);
            setRunId(null);
            const ctl = new AbortController();
            abortRef.current = ctl;
            try {
                const res = await fetch(url, {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify(body),
                    signal: ctl.signal,
                });
                await consume(res);
            } catch (e) {
                if ((e as Error).name !== "AbortError") {
                    setError((e as Error).message);
                }
            } finally {
                setIsRunning(false);
            }
        },
        [consume],
    );

    const resume = useCallback(
        async (url: string) => {
            setIsRunning(true);
            setError(null);
            const ctl = new AbortController();
            abortRef.current = ctl;
            try {
                const res = await fetch(url, {
                    method: "GET",
                    signal: ctl.signal,
                });
                await consume(res);
            } catch (e) {
                if ((e as Error).name !== "AbortError") {
                    setError((e as Error).message);
                }
            } finally {
                setIsRunning(false);
            }
        },
        [consume],
    );

    const reset = useCallback(() => {
        abortRef.current?.abort();
        setEvents([]);
        setError(null);
        setIsRunning(false);
        setRunId(null);
    }, []);

    // Abort the in-flight fetch when the component unmounts so stale closures
    // don't fire onEvent callbacks (e.g. revalidateEntityPath) after the user
    // has navigated away.
    useEffect(() => {
        return () => {
            abortRef.current?.abort();
        };
    }, []);

    return {events, isRunning, error, runId, run, resume, reset};
}
