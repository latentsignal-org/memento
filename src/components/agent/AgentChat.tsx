"use client";
import {useEffect, useRef, useState} from "react";
import Link from "next/link";
import ReactMarkdown, {type Components} from "react-markdown";
import remarkGfm from "remark-gfm";
import {
    Bot,
    CheckCircle2,
    ChevronDown,
    CircleAlert,
    LoaderCircle,
    MailOpen,
    PackageCheck,
    Search,
    Send,
    User,
    Users,
    Wrench,
} from "lucide-react";
import type {AgentEvent} from "./useAgentStream";
import {getToolLabel} from "@/lib/tool-labels";
import MentionInput from "@/components/agent/MentionInput";
import type {ContextRef} from "@/lib/context-refs";

export interface ChatTurn {
    role: "user" | "assistant";
    text: string;
    toolCalls?: Array<{ name: string; args: unknown; result?: unknown }>;
    completedAnswer?: string;
}

interface AgentChatProps {
    history: ChatTurn[];                    // committed turns (from server transcript)
    liveEvents: AgentEvent[];               // events streaming for the in-progress turn
    isRunning: boolean;
    error: string | null;
    onSend: (message: string, contextRefs?: ContextRef[]) => void;
    disabled?: boolean;
    failureContext?: "collector" | "memento";
    placeholder?: string;
    submitLabel?: string;
    submittingLabel?: string;
    enableMentions?: boolean;
}

// Fold the streaming events into a single "in-progress assistant turn" view.
function foldLive(events: AgentEvent[]): ChatTurn | null {
    if (events.length === 0) return null;
    let text = "";
    let separateNextText = false;
    const toolCalls: NonNullable<ChatTurn["toolCalls"]> = [];
    for (const e of events) {
        if (e.type === "text_delta") {
            if (separateNextText && text.trim() && e.text.trim()) {
                text += "\n\n";
            }
            text += e.text;
            separateNextText = false;
        } else if (e.type === "tool_call_start") toolCalls.push({name: e.name, args: e.args});
        else if (e.type === "tool_call_result") {
            const last = toolCalls.findLast?.((t) => t.name === e.name && t.result === undefined)
                ?? [...toolCalls].reverse().find((t) => t.name === e.name && t.result === undefined);
            if (last) last.result = e.result;
            separateNextText = true;
        }
    }
    return {role: "assistant", text, toolCalls};
}

type ContextLoadedEvent = Extract<AgentEvent, { type: "context_loaded" }>;

function isContextLoadedEvent(event: AgentEvent): event is ContextLoadedEvent {
    return event.type === "context_loaded";
}

export default function AgentChat({
                                      history,
                                      liveEvents,
                                      isRunning,
                                      error,
                                      onSend,
                                      disabled,
                                      failureContext = "collector",
                                      placeholder,
                                      submitLabel = "Send",
                                      submittingLabel = "Sending",
                                      enableMentions = false,
                                  }: AgentChatProps) {
    const [draft, setDraft] = useState("");
    const [draftRefs, setDraftRefs] = useState<ContextRef[]>([]);
    const scrollRef = useRef<HTMLDivElement>(null);
    const live = foldLive(liveEvents);
    const toolCount = liveEvents.filter((e) => e.type === "tool_call_start").length;
    const loadedContext = [...liveEvents].reverse().find(isContextLoadedEvent);
    const streamedError = [...liveEvents].reverse().find((e) => e.type === "error");
    const effectiveError = error ?? streamedError?.message ?? null;

    useEffect(() => {
        scrollRef.current?.scrollTo({top: scrollRef.current.scrollHeight, behavior: "smooth"});
    }, [history, liveEvents]);

    return (
        <section
            className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-outline-variant/50 bg-surface-container-lowest shadow-sm">
            <header
                className="flex items-center justify-between border-b border-outline-variant/40 bg-surface-container-low px-4 py-3">
                <div className="min-w-0">
                    <div className="text-label-caps font-label-caps text-on-surface-variant">Collector</div>
                    <div className="mt-0.5 text-sm font-semibold text-on-surface">Archive search session</div>
                </div>
                <div className="flex items-center gap-2">
                    {toolCount > 0 && (
                        <span
                            className="rounded border border-outline-variant/60 bg-background px-2 py-1 font-mono text-[11px] text-on-surface-variant">
              {toolCount} tools
            </span>
                    )}
                    <span
                        className={`inline-flex items-center gap-1.5 rounded border px-2.5 py-1 text-[11px] font-medium ${
                            effectiveError
                                ? "border-red-200 bg-red-50 text-red-700"
                                : "border-outline-variant/60 bg-background text-on-surface-variant"
                        }`}
                    >
            {effectiveError ? (
                <CircleAlert className="h-3.5 w-3.5"/>
            ) : isRunning ? (
                <LoaderCircle className="h-3.5 w-3.5 animate-spin"/>
            ) : (
                <CheckCircle2 className="h-3.5 w-3.5 text-primary"/>
            )}
                        {effectiveError ? "Failed" : isRunning ? "Working" : "Ready"}
          </span>
                </div>
            </header>

            {toolCount > 0 && (
                <AgentActivityStrip
                    events={liveEvents}
                    isRunning={isRunning}
                    error={effectiveError}
                    failureContext={failureContext}
                />
            )}
            {loadedContext && (
                <div className="border-b border-outline-variant/35 bg-primary-fixed/30 px-4 py-2">
                    <div className="flex flex-wrap items-center gap-2">
            <span className="text-[11px] font-bold uppercase tracking-[0.08em] text-primary">
              Context loaded
            </span>
                        {loadedContext.refs.map((ref, index) => (
                            <span
                                key={`${ref.kind ?? "context"}:${ref.slug ?? ref.person_id ?? ref.session_id ?? index}`}
                                className="rounded-full border border-primary/20 bg-background px-2 py-0.5 text-xs font-medium text-on-surface"
                            >
                {ref.kind === "person" ? "@" : "#"}{ref.label}
              </span>
                        ))}
                        {loadedContext.warnings?.map((warning) => (
                            <span key={warning} className="text-xs text-on-surface-variant">
                {warning}
              </span>
                        ))}
                    </div>
                </div>
            )}

            <div ref={scrollRef} className="flex-1 space-y-5 overflow-y-auto px-4 py-5">
                {history.length === 0 && !live && (
                    <div
                        className="rounded-lg border border-dashed border-outline-variant/70 bg-surface-container-low px-4 py-5">
                        <div className="flex items-start gap-3">
                            <div
                                className="flex h-8 w-8 shrink-0 items-center justify-center rounded bg-primary-fixed text-on-primary-fixed-variant">
                                <Bot className="h-4 w-4"/>
                            </div>
                            <div className="space-y-1">
                                <div className="text-sm font-semibold text-on-surface">Start a collection</div>
                                <p className="max-w-[42rem] text-sm leading-6 text-on-surface-variant">
                                    Name the topic, person, company, or date range you want to gather into a project.
                                </p>
                            </div>
                        </div>
                    </div>
                )}
                {history.map((t, i) => (
                    <TurnView key={`h-${i}`} turn={t}/>
                ))}
                {live && <TurnView turn={live} pending={isRunning}/>}
                {effectiveError && (
                    <AgentErrorMessage message={effectiveError} failureContext={failureContext}/>
                )}
            </div>
            <form
                className="flex gap-2 border-t border-outline-variant/40 bg-surface-container-low px-3 py-3"
                onSubmit={(e) => {
                    e.preventDefault();
                    const trimmed = draft.trim();
                    if (!trimmed || isRunning || disabled) return;
                    onSend(trimmed, draftRefs);
                    setDraft("");
                    setDraftRefs([]);
                }}
            >
                {enableMentions ? (
                    <MentionInput
                        value={draft}
                        refs={draftRefs}
                        onChange={(nextValue, nextRefs) => {
                            setDraft(nextValue);
                            setDraftRefs(nextRefs);
                        }}
                        inputClassName="w-full min-w-0 rounded border border-outline-variant/70 bg-background px-3 py-2 text-sm text-on-surface shadow-inner focus:outline-none focus:ring-2 focus:ring-primary/30"
                        placeholder={placeholder ?? "Tell the collector what to find..."}
                        disabled={isRunning || disabled}
                    />
                ) : (
                    <input
                        type="text"
                        className="min-w-0 flex-1 rounded border border-outline-variant/70 bg-background px-3 py-2 text-sm text-on-surface shadow-inner focus:outline-none focus:ring-2 focus:ring-primary/30"
                        placeholder={placeholder ?? "Tell the collector what to find..."}
                        value={draft}
                        onChange={(e) => setDraft(e.target.value)}
                        disabled={isRunning || disabled}
                    />
                )}
                <button
                    type="submit"
                    aria-label={isRunning ? submittingLabel : submitLabel}
                    className="inline-flex h-10 items-center gap-2 rounded bg-primary px-3 sm:px-4 text-sm font-semibold text-white shadow-sm transition hover:opacity-90 disabled:opacity-50"
                    disabled={isRunning || disabled || !draft.trim()}
                >
                    {isRunning ? (
                        <LoaderCircle className="h-4 w-4 animate-spin"/>
                    ) : (
                        <Send className="h-4 w-4"/>
                    )}
                    <span className="hidden sm:inline">{isRunning ? submittingLabel : submitLabel}</span>
                </button>
            </form>
        </section>
    );
}

function AgentActivityStrip({
                                events,
                                isRunning,
                                error,
                                failureContext,
                            }: {
    events: AgentEvent[];
    isRunning: boolean;
    error: string | null;
    failureContext: "collector" | "memento";
}) {
    const stages = [
        {
            label: "Search",
            tools: ["fts_search"],
            icon: Search,
        },
        {
            label: "Inspect",
            tools: ["get_thread", "get_message"],
            icon: MailOpen,
        },
        {
            label: "Resolve",
            tools: ["find_people"],
            icon: Users,
        },
        {
            label: "Stage",
            tools: ["propose_bundle"],
            icon: PackageCheck,
        },
    ];
    const starts = events.filter((e) => e.type === "tool_call_start");
    const results = events.filter((e) => e.type === "tool_call_result");
    const latestTool = [...starts].reverse()[0]?.name;
    const failed = Boolean(error);

    return (
        <div className="border-b border-outline-variant/35 bg-background px-4 py-3">
            {failed && (
                <div className="mb-3 rounded border border-red-200 bg-red-50 px-3 py-2 text-xs leading-5 text-red-700">
                    {failureContext === "memento"
                        ? "Memento stopped before finishing this answer. The context panel will update after a successful run."
                        : "The collector stopped before finishing. The bundle preview will update after a successful staging run."}
                </div>
            )}
            <div className="grid grid-cols-4 gap-2">
                {stages.map((stage) => {
                    const started = starts.filter((e) => stage.tools.includes(e.name)).length;
                    const completed = results.filter((e) => stage.tools.includes(e.name)).length;
                    const active = isRunning && latestTool && stage.tools.includes(latestTool) && completed < started;
                    const Icon = stage.icon;
                    return (
                        <div
                            key={stage.label}
                            className={`min-w-0 rounded border px-2.5 py-2 transition ${
                                failed && started === 0
                                    ? "border-red-200 bg-red-50 text-red-700"
                                    : active
                                        ? "border-primary/40 bg-primary-fixed text-on-primary-fixed-variant"
                                        : started > 0
                                            ? "border-outline-variant/50 bg-surface-container-low text-on-surface"
                                            : "border-outline-variant/30 bg-surface-container-lowest text-on-surface-variant"
                            }`}
                        >
                            <div className="flex items-center gap-1.5">
                                {active ? (
                                    <LoaderCircle className="h-3.5 w-3.5 shrink-0 animate-spin"/>
                                ) : (
                                    <Icon className="h-3.5 w-3.5 shrink-0"/>
                                )}
                                <span className="truncate text-[11px] font-semibold">{stage.label}</span>
                            </div>
                            <div className="mt-1 font-mono text-[10px] opacity-75">
                                {started > 0 ? `${completed}/${started}` : failed ? "blocked" : "waiting"}
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}

function AgentErrorMessage({
                               message,
                               failureContext,
                           }: {
    message: string;
    failureContext: "collector" | "memento";
}) {
    const copy = explainAgentError(message, failureContext);

    return (
        <div className="min-w-0 overflow-hidden rounded border border-red-200 bg-red-50 p-3 text-sm text-red-800">
            <div className="flex min-w-0 items-start gap-2">
                <CircleAlert className="mt-0.5 h-4 w-4 shrink-0"/>
                <div className="min-w-0 space-y-1">
                    <div className="font-semibold">{copy.title}</div>
                    <p className="break-words leading-6 [overflow-wrap:anywhere]">{copy.body}</p>
                    <details className="pt-1 text-xs text-red-700">
                        <summary className="cursor-pointer select-none font-semibold">Technical details</summary>
                        <pre
                            className="mt-2 max-h-36 overflow-auto whitespace-pre-wrap break-words rounded border border-red-200 bg-white/65 p-2 font-mono text-[11px] leading-5 [overflow-wrap:anywhere]">
              {message}
            </pre>
                    </details>
                </div>
            </div>
        </div>
    );
}

function explainAgentError(
    message: string,
    failureContext: "collector" | "memento",
): { title: string; body: string } {
    const lower = message.toLowerCase();
    const title =
        failureContext === "memento"
            ? "Memento could not finish this answer."
            : "Collector run stopped before staging a bundle.";

    if (lower === "cancelled" || lower.includes("cancelled") || lower.includes("canceled")) {
        return {
            title,
            body: "The run was cancelled before it completed.",
        };
    }

    if (
        lower.includes("too many requests") ||
        lower.includes("rate limit") ||
        lower.includes("tokens per minute") ||
        lower.includes("token_quota") ||
        lower.includes("too_many_tokens")
    ) {
        return {
            title,
            body: "The model provider hit a rate or token limit while processing this request. Wait a moment and try again, or ask a narrower follow-up.",
        };
    }

    if (lower.includes("context deadline exceeded") || lower.includes("timeout") || lower.includes("timed out")) {
        return {
            title,
            body: "The request took too long and timed out before Memento could finish. Try again with a narrower question.",
        };
    }

    if (lower.includes("connection refused") || lower.includes("failed to fetch") || lower.includes("network")) {
        return {
            title,
            body: "Memento could not reach the model service. Check that the configured model provider is running, then try again.",
        };
    }

    return {
        title,
        body: "Something interrupted the run before it completed. The technical details below include the provider error for debugging.",
    };
}

export function MessagePill({messageId}: { messageId: number }) {
    const [data, setData] = useState<{
        subject: string;
        from_name: string;
        from_email: string;
        sent_at: string;
        snippet: string;
        external_url?: string;
    } | null>(null);
    const [loading, setLoading] = useState(false);
    const [hovered, setHovered] = useState(false);

    useEffect(() => {
        // Fetch the message once per id. The effect must NOT depend on `data`
        // or `loading`: a failed request leaves `data` null and resets
        // `loading` to false, which — with those in the dependency array —
        // re-satisfies the old guard and refetches in an unbounded loop that
        // hammers /api/messages/:id. A `cancelled` flag avoids a state update
        // after unmount or a messageId change mid-flight.
        let cancelled = false;
        window.queueMicrotask(() => {
            if (!cancelled) setLoading(true);
        });
        fetch(`/api/messages/${messageId}`)
            .then((r) => {
                if (!r.ok) throw new Error(`failed to fetch message ${messageId}: ${r.status}`);
                return r.json();
            })
            .then((d) => {
                if (cancelled) return;
                setData(d);
                setLoading(false);
            })
            .catch((err) => {
                if (cancelled) return;
                console.error(err);
                setLoading(false);
            });
        return () => {
            cancelled = true;
        };
    }, [messageId]);

    return (
        <span
            className="relative inline-block align-baseline mx-0.5"
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
        >
      {/* Click toggles the preview so the pill works on touch screens */}
            <span
                onClick={() => setHovered((current) => !current)}
                className="inline-flex items-center gap-1 rounded bg-primary/10 hover:bg-primary/20 px-2 py-0.5 text-[11px] font-semibold text-primary cursor-pointer transition"
            >
        <MailOpen className="h-3 w-3 shrink-0"/>
                {data ? data.subject : `Message #${messageId}`}
      </span>

            {hovered && (
                <span className="absolute left-0 bottom-full z-50 pb-2 w-72 block">
          <span
              className="rounded-lg border border-outline-variant bg-surface-container-lowest p-3 shadow-xl text-left block text-on-surface">
            {loading ? (
                <span className="flex items-center gap-2 text-xs text-on-surface-variant">
                <LoaderCircle className="h-3.5 w-3.5 animate-spin"/>
                Loading preview...
              </span>
            ) : data ? (
                <span className="block space-y-1.5">
                <span className="block text-xs font-bold truncate">{data.subject}</span>
                <span className="block text-[11px] text-on-surface-variant truncate">
                  From: {data.from_name || data.from_email}
                </span>
                    {data.sent_at && (
                        <span className="block text-[10px] font-mono text-on-surface-variant">
                    {data.sent_at.slice(0, 16).replace("T", " ")}
                  </span>
                    )}
                    <span
                        className="block text-xs text-on-surface-variant/90 line-clamp-3 bg-surface-container-low p-1.5 rounded border border-outline-variant/40 whitespace-normal leading-normal">
                  {data.snippet}
                </span>
                    {data.external_url && (
                        <a
                            href={data.external_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-[10px] font-semibold text-primary hover:underline pt-1"
                        >
                            Open in Gmail
                        </a>
                    )}
              </span>
            ) : (
                <span className="text-xs text-red-500">Failed to load preview</span>
            )}
          </span>
        </span>
            )}
    </span>
    );
}

// Markdown component overrides. We delegate parsing to react-markdown +
// remark-gfm (which adds tables, strikethrough, task lists, etc.) and only
// style the output, while preserving the two custom behaviors the chat needs:
// `[msg:N]` message pills and internal navigation via Next's <Link>.
const markdownComponents: Components = {
    p: ({children}) => <p className="text-sm leading-6 mb-3 last:mb-0">{children}</p>,
    ul: ({children}) => <ul className="list-disc pl-5 space-y-1.5 my-2 text-sm leading-6">{children}</ul>,
    ol: ({children}) => <ol className="list-decimal pl-5 space-y-1.5 my-2 text-sm leading-6">{children}</ol>,
    li: ({children}) => <li className="text-sm leading-6">{children}</li>,
    h1: ({children}) => <h1 className="mt-3 mb-2 text-base font-bold text-on-surface first:mt-0">{children}</h1>,
    h2: ({children}) => <h2 className="mt-3 mb-1.5 text-sm font-bold text-on-surface first:mt-0">{children}</h2>,
    h3: ({children}) => <h3 className="mt-2 mb-1 text-sm font-semibold text-on-surface first:mt-0">{children}</h3>,
    h4: ({children}) => <h4
        className="mt-2 mb-1 text-sm font-semibold text-on-surface-variant first:mt-0">{children}</h4>,
    h5: ({children}) => <h5
        className="mt-2 mb-1 text-xs font-semibold text-on-surface-variant first:mt-0">{children}</h5>,
    h6: ({children}) => <h6
        className="mt-2 mb-1 text-xs font-semibold text-on-surface-variant first:mt-0">{children}</h6>,
    strong: ({children}) => <strong className="font-semibold text-on-surface">{children}</strong>,
    em: ({children}) => <em className="italic">{children}</em>,
    del: ({children}) => <del className="line-through opacity-70">{children}</del>,
    hr: () => <hr className="my-3 border-outline-variant/40"/>,
    blockquote: ({children}) => (
        <blockquote className="my-2 border-l-2 border-outline-variant/50 pl-3 italic text-on-surface-variant">
            {children}
        </blockquote>
    ),
    pre: ({children}) => (
        <pre
            className="my-2 overflow-x-auto rounded border border-outline-variant/40 bg-surface-container-low p-3 text-xs leading-relaxed">
      {children}
    </pre>
    ),
    code: ({children, className}) => {
        // Block code (fenced or multi-line) lives inside <pre>; only inline code
        // gets the pill background so we don't double up styling.
        const isBlock = (className?.includes("language-") ?? false) || String(children).includes("\n");
        if (isBlock) {
            return <code className={`font-mono ${className ?? ""}`}>{children}</code>;
        }
        return (
            <code className="rounded bg-surface-container px-1 py-0.5 font-mono text-[0.85em] text-on-surface">
                {children}
            </code>
        );
    },
    table: ({children}) => (
        <div className="my-2 overflow-x-auto">
            <table className="w-full border-collapse text-sm">{children}</table>
        </div>
    ),
    th: ({children}) => (
        <th className="border border-outline-variant/40 bg-surface-container-low px-2.5 py-1.5 text-left text-xs font-semibold text-on-surface">
            {children}
        </th>
    ),
    td: ({children}) => (
        <td className="border border-outline-variant/40 px-2.5 py-1.5 align-top text-sm">{children}</td>
    ),
    a: ({href, children}) => {
        // `[msg:N]` is preprocessed into a link with an `msg:N` href below.
        if (href?.startsWith("msg:")) {
            const messageId = parseInt(href.slice(4), 10);
            if (Number.isFinite(messageId)) {
                return <MessagePill messageId={messageId}/>;
            }
        }
        if (href?.startsWith("/")) {
            return (
                <Link href={href} className="font-semibold text-primary hover:underline">
                    {children}
                </Link>
            );
        }
        return (
            <a
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                className="font-semibold text-primary hover:underline"
            >
                {children}
            </a>
        );
    },
};

function renderText(text: string): React.ReactNode {
    if (!text) return "";
    // Turn bare `[msg:123]` tokens into real markdown links so react-markdown
    // parses them; the `a` override renders them as MessagePill. The negative
    // lookahead avoids touching tokens that already carry a `(…)` target.
    const withMsgLinks = text.replace(/\[msg:(\d+)\](?!\()/g, "[msg:$1](msg:$1)");
    return (
        <div className="space-y-1.5">
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                {withMsgLinks}
            </ReactMarkdown>
        </div>
    );
}

function TurnView({turn, pending}: { turn: ChatTurn; pending?: boolean }) {
    const isUser = turn.role === "user";
    return (
        <div className={`flex items-start gap-3 ${isUser ? "justify-end" : "justify-start"}`}>
            {!isUser && (
                <div
                    className="mt-1 flex h-7 w-7 shrink-0 items-center justify-center rounded bg-primary-fixed text-on-primary-fixed-variant">
                    <Bot className="h-3.5 w-3.5"/>
                </div>
            )}
            <div
                className={`max-w-[82%] rounded-lg px-3.5 py-3 shadow-sm ${
                    isUser
                        ? "bg-primary text-white"
                        : "border border-outline-variant/45 bg-background text-on-surface"
                }`}
            >
                {turn.toolCalls && turn.toolCalls.length > 0 && (
                    <ToolCallList calls={turn.toolCalls}/>
                )}
                {(turn.text || (pending && !turn.toolCalls?.length)) ? (
                    <div className="text-sm leading-6">
                        {turn.text ? renderText(turn.text) : "Thinking…"}
                    </div>
                ) : (!isUser && !pending && !turn.toolCalls?.length && (
                    <div className="text-sm leading-6 italic text-on-surface-variant">
                        Research complete — check the bundle panel on the right.
                    </div>
                ))}
            </div>
            {isUser && (
                <div
                    className="mt-1 flex h-7 w-7 shrink-0 items-center justify-center rounded bg-surface-container-high text-on-surface-variant">
                    <User className="h-3.5 w-3.5"/>
                </div>
            )}
        </div>
    );
}

function ToolCallList({calls}: { calls: NonNullable<ChatTurn["toolCalls"]> }) {
    return (
        <details className="group mb-2 rounded border border-outline-variant/40 bg-surface-container-low/70 text-xs">
            <summary
                className="flex cursor-pointer select-none items-center justify-between gap-3 px-2.5 py-2 text-on-surface-variant">
        <span className="inline-flex items-center gap-1.5">
          <Wrench className="h-3.5 w-3.5"/>
            {calls.length} tool call{calls.length === 1 ? "" : "s"}
        </span>
                <ChevronDown className="h-3.5 w-3.5 transition group-open:rotate-180"/>
            </summary>
            <ul className="space-y-1 border-t border-outline-variant/30 px-2.5 py-2">
                {calls.map((c, i) => (
                    <li key={i} className="min-w-0 truncate font-mono text-[11px] text-on-surface-variant">
                        <span className="font-semibold text-primary">{getToolLabel(c.name, c.args)}</span>
                        <span className="opacity-70"> ({summarize(c.args)})</span>
                        {c.result !== undefined && (
                            <span className="ml-1 opacity-70"> → {summarize(c.result)}</span>
                        )}
                    </li>
                ))}
            </ul>
        </details>
    );
}

function summarize(v: unknown): string {
    try {
        const s = JSON.stringify(v);
        if (!s) return "";
        if (s.length <= 80) return s;
        return s.slice(0, 77) + "…";
    } catch {
        return String(v);
    }
}
