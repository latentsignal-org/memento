"use client";
import {useCallback, useEffect, useRef, useState} from "react";
import Link from "next/link";
import {usePathname, useRouter, useSearchParams} from "next/navigation";
import {ArrowLeft, MoreVertical,} from "lucide-react";
import {useAgentStream} from "@/components/agent/useAgentStream";
import AgentChat, {type ChatTurn} from "@/components/agent/AgentChat";
import SidePanel from "@/components/agent/SidePanel";
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,} from "@/components/ui/dropdown-menu";
import {type ContextRef, decodeContextRefs} from "@/lib/context-refs";

type AskSessionIdentity = {
    id: number;
    slug: string;
};

type AskSessionDetailResponse = {
    session: AskSessionIdentity;
    turns: Array<{
        user_message: string;
        assistant_answer: string;
        status: string;
    }>;
};

function parsePositiveID(raw: string | null): number | null {
    if (!raw) return null;
    const parsed = Number.parseInt(raw, 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
}

function askRunIDFromParams(params: { get(name: string): string | null }): number | null {
    if (params.get("agentFlow") !== "ask") return null;
    return parsePositiveID(params.get("agentRunId"));
}

function historyFromSessionTurns(turns: AskSessionDetailResponse["turns"]): ChatTurn[] {
    const restored: ChatTurn[] = [];
    for (const turn of turns) {
        restored.push({role: "user", text: turn.user_message});
        if (turn.status === "complete") {
            restored.push({
                role: "assistant",
                text: turn.assistant_answer,
                completedAnswer: turn.assistant_answer,
            });
        }
    }
    return restored;
}

export default function AskMementoClient() {
    const searchParams = useSearchParams();
    const router = useRouter();
    const pathname = usePathname();

    const [history, setHistory] = useState<ChatTurn[]>([]);
    const [previousInteractionId, setPreviousInteractionId] = useState<string | undefined>(undefined);
    // On small screens the chat and context panes don't fit side by side;
    // a toggle shows one at a time (both stay mounted to preserve state).
    const [mobilePane, setMobilePane] = useState<"chat" | "context">("chat");
    const [, setAskSessionState] = useState<AskSessionIdentity | null>(null);
    const askSessionRef = useRef<AskSessionIdentity | null>(null);
    const resumeStartedRef = useRef(false);
    const sessionRestoreStartedRef = useRef(false);
    const initialQuerySentRef = useRef(false);
    const initialQueryRef = useRef(searchParams.get("q") || "");
    const initialContextRefsRef = useRef<ContextRef[]>(decodeContextRefs(searchParams.get("refs")));
    const initialSessionSlugRef = useRef(searchParams.get("session"));
    const initialRunIDRef = useRef(askRunIDFromParams(searchParams));

    const setAskSession = useCallback((session: AskSessionIdentity | null) => {
        askSessionRef.current = session;
        setAskSessionState(session);
    }, []);

    const replaceUrlParams = useCallback((mutate: (params: URLSearchParams) => void) => {
        const params = new URLSearchParams(window.location.search);
        mutate(params);
        const qs = params.toString();
        const nextURL = qs ? `${pathname}?${qs}` : pathname;
        window.history.replaceState(null, "", nextURL);
        router.replace(nextURL, {scroll: false});
    }, [pathname, router]);

    const clearAskRunURLParams = useCallback(() => {
        replaceUrlParams((params) => {
            if (askSessionRef.current?.slug) {
                params.set("session", askSessionRef.current.slug);
            }
            if (params.get("agentFlow") === "ask") {
                params.delete("agentFlow");
                params.delete("agentRunId");
            }
            params.delete("q");
            params.delete("refs");
        });
    }, [replaceUrlParams]);

    const handleResponseHeaders = useCallback((headers: Headers) => {
        const runID = parsePositiveID(headers.get("X-Memento-Agent-Run-ID"));
        const askSessionID = parsePositiveID(headers.get("X-Memento-Ask-Session-ID"));
        const askSessionSlug = headers.get("X-Memento-Ask-Session-Slug");
        const askTurnID = parsePositiveID(headers.get("X-Memento-Ask-Turn-ID"));
        if (!runID || !askSessionID || !askSessionSlug || !askTurnID) return;

        setAskSession({id: askSessionID, slug: askSessionSlug});
        replaceUrlParams((params) => {
            params.set("session", askSessionSlug);
            params.set("agentFlow", "ask");
            params.set("agentRunId", String(runID));
            params.delete("q");
            params.delete("refs");
        });
    }, [replaceUrlParams, setAskSession]);

    const {events, isRunning, error, run, resume, reset} = useAgentStream(
        undefined,
        undefined,
        handleResponseHeaders,
    );

    // Define triggerChat here before useEffect that uses it
    const triggerChat = useCallback(
        (message: string, contextRefs: ContextRef[] = []) => {
            const trimmed = message.trim();
            if (!trimmed || isRunning) return;

            setHistory((prev) => [...prev, {role: "user", text: trimmed}]);
            run(`/api/agents/memento/turn`, {
                message: trimmed,
                ask_session_id: askSessionRef.current?.id,
                previous_interaction_id: previousInteractionId,
                history: history.map((turn) => ({role: turn.role, content: turn.text})),
                context_refs: contextRefs,
            });
        },
        [history, isRunning, previousInteractionId, run]
    );

    // Auto-send initial query if provided
    useEffect(() => {
        const initialQuery = initialQueryRef.current;
        if (initialSessionSlugRef.current || initialRunIDRef.current) return;
        if (!initialQuery || initialQuerySentRef.current) return;
        const timer = window.setTimeout(() => {
            if (initialQuerySentRef.current) return;
            initialQuerySentRef.current = true;
            triggerChat(initialQuery, initialContextRefsRef.current);
        }, 0);
        return () => window.clearTimeout(timer);
    }, [triggerChat]);

    useEffect(() => {
        const sessionSlug = initialSessionSlugRef.current;
        if (!sessionSlug || sessionRestoreStartedRef.current) return;
        sessionRestoreStartedRef.current = true;

        const controller = new AbortController();
        const restore = async () => {
            try {
                const res = await fetch(`/api/sessions/${encodeURIComponent(sessionSlug)}`, {
                    cache: "no-store",
                    signal: controller.signal,
                });
                if (!res.ok) {
                    throw new Error(`HTTP ${res.status}`);
                }
                const data = (await res.json()) as AskSessionDetailResponse;
                setAskSession({id: data.session.id, slug: data.session.slug});
                const restored = historyFromSessionTurns(data.turns);
                setHistory((prev) => (prev.length ? prev : restored));
            } catch (err) {
                if (!controller.signal.aborted) {
                    console.warn("failed to restore ask session", err);
                }
            } finally {
                if (!controller.signal.aborted && initialRunIDRef.current && !resumeStartedRef.current) {
                    resumeStartedRef.current = true;
                    void resume(`/api/agents/runs/${initialRunIDRef.current}/events`);
                }
            }
        };

        void restore();
        return () => {
            controller.abort();
            sessionRestoreStartedRef.current = false;
        };
    }, [resume, setAskSession]);

    useEffect(() => {
        if (initialSessionSlugRef.current) return;
        const runID = initialRunIDRef.current;
        if (!runID || resumeStartedRef.current || isRunning) return;
        resumeStartedRef.current = true;
        void resume(`/api/agents/runs/${runID}/events`);
    }, [isRunning, resume]);

    // Handle run completion to commit turn to history
    useEffect(() => {
        if (isRunning) return;
        const done = events.find((e) => e.type === "done");
        if (!done) return;

        let text = "";
        let completedAnswer = "";
        let separateNextText = false;
        let separateNextCompletedText = false;
        const toolCalls: NonNullable<ChatTurn["toolCalls"]> = [];
        for (const e of events) {
            if (e.type === "text_delta") {
                if (separateNextText && text.trim() && e.text.trim()) {
                    text += "\n\n";
                }
                text += e.text;
                separateNextText = false;

                if (separateNextCompletedText && completedAnswer.trim() && e.text.trim()) {
                    completedAnswer += "\n\n";
                }
                completedAnswer += e.text;
                separateNextCompletedText = false;
            } else if (e.type === "tool_call_start") {
                toolCalls.push({name: e.name, args: e.args});
                completedAnswer = "";
            } else if (e.type === "tool_call_result") {
                const last = toolCalls.findLast?.((t) => t.name === e.name && t.result === undefined)
                    ?? [...toolCalls].reverse().find((t) => t.name === e.name && t.result === undefined);
                if (last) last.result = e.result;
                separateNextText = true;
                separateNextCompletedText = true;
                completedAnswer = "";
            }
        }

        window.queueMicrotask(() => {
            setHistory((prev) => [...prev, {role: "assistant", text, toolCalls, completedAnswer}]);
            setPreviousInteractionId(done.interaction_id);
            clearAskRunURLParams();
            reset();
        });
    }, [clearAskRunURLParams, events, isRunning, reset]);

    const handleResetChat = () => {
        reset();
        replaceUrlParams((params) => {
            if (params.get("agentFlow") === "ask") {
                params.delete("agentFlow");
                params.delete("agentRunId");
            }
            params.delete("session");
            params.delete("q");
            params.delete("refs");
        });
        setAskSession(null);
        setHistory([]);
        setPreviousInteractionId(undefined);
    };

    const latestAssistantTurn = [...history].reverse().find((turn) => turn.role === "assistant");
    const latestUserTurn = [...history].reverse().find((turn) => turn.role === "user");

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div className="w-full max-w-[1440px] mx-auto px-4 sm:px-6 py-8 sm:py-10 space-y-8">

                {/* Header Section */}
                <header className="space-y-3">
                    <div className="flex items-center gap-3">
                        <Link
                            href="/home"
                            className="inline-flex items-center gap-2 text-sm font-semibold text-on-surface-variant hover:text-primary transition"
                        >
                            <ArrowLeft className="h-4 w-4"/>
                            Back to Home
                        </Link>
                    </div>
                    <div className="max-w-[820px]">
                        <h1 className="text-display-lg font-display-lg text-primary max-sm:text-[32px]">Ask Memento</h1>
                    </div>
                </header>

                {/* Chat Layout: Two Pane UI */}
                <div className="space-y-4">
                    <div className="flex items-center justify-between">
                        {/* Mobile pane toggle */}
                        <div
                            className="lg:hidden inline-flex rounded-lg border border-outline-variant/60 bg-surface-container-low p-0.5">
                            {([["chat", "Chat"], ["context", "Context"]] as const).map(([pane, label]) => (
                                <button
                                    key={pane}
                                    type="button"
                                    onClick={() => setMobilePane(pane)}
                                    className={`px-3 py-1.5 rounded-md text-ui-small font-bold transition-colors cursor-pointer ${
                                        mobilePane === pane
                                            ? "bg-primary text-primary-foreground shadow-sm"
                                            : "text-on-surface-variant hover:text-primary"
                                    }`}
                                >
                                    {label}
                                </button>
                            ))}
                        </div>
                        <div className="hidden lg:block"></div>
                        <DropdownMenu>
                            <DropdownMenuTrigger
                                className="inline-flex items-center justify-center h-8 w-8 rounded-md text-on-surface-variant hover:bg-surface-container transition">
                                <MoreVertical className="h-4 w-4"/>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                                <DropdownMenuItem variant="destructive" onClick={handleResetChat}>
                                    Clear chat
                                </DropdownMenuItem>
                            </DropdownMenuContent>
                        </DropdownMenu>
                    </div>

                    <div
                        className="grid h-[calc(100dvh-300px)] lg:h-[calc(100dvh-360px)] max-h-[720px] min-h-[420px] grid-cols-1 gap-5 lg:grid-cols-12">
                        <div
                            className={`min-h-0 lg:col-span-6 xl:col-span-5 ${mobilePane === "chat" ? "" : "max-lg:hidden"}`}>
                            <AgentChat
                                history={history}
                                liveEvents={events}
                                isRunning={isRunning}
                                error={error}
                                onSend={triggerChat}
                                failureContext="memento"
                                placeholder="Ask a follow-up or explore a person/project/concept..."
                                enableMentions
                            />
                        </div>
                        <div
                            className={`min-h-0 lg:col-span-6 xl:col-span-7 ${mobilePane === "context" ? "" : "max-lg:hidden"}`}>
                            <SidePanel
                                liveEvents={events}
                                completedAnswerText={isRunning ? "" : (latestAssistantTurn?.completedAnswer ?? latestAssistantTurn?.text)}
                                completedToolCalls={isRunning ? [] : latestAssistantTurn?.toolCalls}
                                isRunning={isRunning}
                                userQuery={latestUserTurn?.text}
                            />
                        </div>
                    </div>
                </div>
            </div>
        </main>
    );
}
