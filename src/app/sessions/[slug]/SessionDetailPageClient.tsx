"use client";

import {useEffect, useState} from "react";
import Link from "next/link";
import ReactMarkdown, {type Components} from "react-markdown";
import remarkGfm from "remark-gfm";
import {ArrowLeft, ExternalLink, Sparkles} from "lucide-react";
import {apiGet, currentSlug} from "@/lib/api";
import {formatMonthDay, relativeDate} from "@/lib/date-utils";
import SessionActions, {SessionTitleEditor} from "./SessionActions";
import EntityNotFound from "@/components/EntityNotFound";

interface AskSession {
    id: number;
    slug: string;
    title: string;
    summary: string;
    status: string;
    pinned: boolean;
    archived_at?: string;
    created_at: string;
    updated_at: string;
}

interface AskContextRef {
    id: number;
    ask_turn_id: number;
    ref_kind: "person" | "project" | "concept" | "ask_session" | string;
    ref_id: string;
    label: string;
    payload_json: string;
    created_at: string;
}

interface AskTurn {
    id: number;
    ask_session_id: number;
    run_id?: number;
    turn_index: number;
    user_message: string;
    assistant_answer: string;
    answer_summary: string;
    status: string;
    cited_message_ids_json: string;
    cited_message_ids: number[];
    context_refs: AskContextRef[];
    run_status?: string;
    tool_summary_json: string;
    created_at: string;
    updated_at: string;
}

interface AskSessionDetail {
    session: AskSession;
    turns: AskTurn[];
}

async function loadSession(slug: string): Promise<AskSessionDetail | null> {
    return apiGet<AskSessionDetail>(`/api/sessions/${slug}`);
}

function parsePayload(ref: AskContextRef): Record<string, unknown> {
    try {
        const parsed = JSON.parse(ref.payload_json || "{}") as Record<string, unknown>;
        return parsed && typeof parsed === "object" ? parsed : {};
    } catch {
        return {};
    }
}

function refHref(ref: AskContextRef): string | null {
    const payload = parsePayload(ref);
    const slug = typeof payload.slug === "string" ? payload.slug : "";
    if (ref.ref_kind === "person" && slug) return `/people/${slug}`;
    if (ref.ref_kind === "project" && slug) return `/projects/${slug}`;
    if (ref.ref_kind === "concept" && slug) return `/concepts/${slug}`;
    if (ref.ref_kind === "ask_session" && slug) return `/sessions/${slug}`;
    return null;
}

function refPrefix(kind: string): "@" | "#" {
    return kind === "person" ? "@" : "#";
}

function refKindLabel(kind: string): string {
    switch (kind) {
        case "person":
            return "Person";
        case "project":
            return "Project";
        case "concept":
            return "Concept";
        case "ask_session":
            return "Session";
        default:
            return kind;
    }
}

function cleanAnswer(text: string): string {
    return text.replace(/\s*\[msg:\d+\]/g, "").trim();
}

function statusClasses(status: string) {
    switch (status) {
        case "complete":
            return "border-primary/20 bg-primary-fixed/70 text-primary";
        case "failed":
            return "border-red-200 bg-red-50 text-red-700";
        case "running":
            return "border-outline-variant/60 bg-surface-container-high text-on-surface-variant";
        default:
            return "border-outline-variant/60 bg-surface-container-high text-on-surface-variant";
    }
}

const markdownComponents: Components = {
    p: ({children}) => <p className="mb-3 text-sm leading-7 last:mb-0">{children}</p>,
    ul: ({children}) => <ul className="my-3 list-disc space-y-1.5 pl-5 text-sm leading-7">{children}</ul>,
    ol: ({children}) => <ol className="my-3 list-decimal space-y-1.5 pl-5 text-sm leading-7">{children}</ol>,
    li: ({children}) => <li className="text-sm leading-7">{children}</li>,
    h1: ({children}) => <h1 className="mb-2 mt-4 text-lg font-bold text-on-surface first:mt-0">{children}</h1>,
    h2: ({children}) => <h2 className="mb-2 mt-4 text-base font-bold text-on-surface first:mt-0">{children}</h2>,
    h3: ({children}) => <h3 className="mb-1.5 mt-3 text-sm font-semibold text-on-surface first:mt-0">{children}</h3>,
    h4: ({children}) => <h4
        className="mb-1.5 mt-3 text-sm font-semibold text-on-surface-variant first:mt-0">{children}</h4>,
    strong: ({children}) => <strong className="font-semibold text-on-surface">{children}</strong>,
    em: ({children}) => <em className="italic">{children}</em>,
    blockquote: ({children}) => (
        <blockquote className="my-3 border-l-2 border-outline-variant/60 pl-3 text-on-surface-variant">
            {children}
        </blockquote>
    ),
    code: ({children, className}) => {
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
    pre: ({children}) => (
        <pre
            className="my-3 overflow-x-auto rounded border border-outline-variant/40 bg-surface-container-low p-3 text-xs leading-relaxed">
      {children}
    </pre>
    ),
    a: ({href, children}) => {
        if (href?.startsWith("/")) {
            return (
                <Link href={href} className="font-semibold text-primary hover:underline">
                    {children}
                </Link>
            );
        }
        return (
            <a href={href} target="_blank" rel="noopener noreferrer"
               className="font-semibold text-primary hover:underline">
                {children}
            </a>
        );
    },
};

function MarkdownText({text, compact = false}: { text: string; compact?: boolean }) {
    if (!text) return null;
    return (
        <div className={compact ? "max-w-[820px] text-on-surface-variant" : "text-on-surface"}>
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents} disallowedElements={["img"]}>
                {text}
            </ReactMarkdown>
        </div>
    );
}

export default function SessionDetailPageClient() {
    const [data, setData] = useState<AskSessionDetail | null | undefined>(undefined);

    useEffect(() => {
        let cancelled = false;
        loadSession(currentSlug()).then((result) => {
            if (cancelled) return;
            if (!result?.session) {
                setData(null);
                return;
            }
            document.title = `${result.session.title || "Ask Session"} | Sessions | Memento`;
            setData(result);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (data === undefined) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    if (data === null) {
        return <EntityNotFound kind="session" backHref="/sessions" backLabel="Back to Sessions"/>;
    }

    const {session, turns} = data;

    return (
        <main className="min-h-screen bg-background pt-16 text-on-surface">
            <div className="mx-auto flex w-full max-w-[1180px] flex-col gap-6 px-4 py-8 sm:px-6 sm:py-10">
                <header className="space-y-5 border-b border-outline-variant pb-6">
                    <div>
                        <Link
                            href="/sessions"
                            className="inline-flex items-center gap-2 text-sm font-medium text-on-surface-variant transition hover:text-primary"
                        >
                            <ArrowLeft className="h-4 w-4"/>
                            Sessions
                        </Link>
                    </div>

                    <div className="space-y-3">
                        <div className="flex flex-wrap items-center justify-between gap-3">
                            <div className="flex flex-wrap items-center gap-2">
                <span
                    className={`rounded-full border px-2.5 py-0.5 text-[11px] font-semibold uppercase tracking-[0.06em] ${statusClasses(session.status)}`}>
                  {session.status || "active"}
                </span>
                                <span className="text-xs text-on-surface-variant">
                  Updated {relativeDate(session.updated_at)}
                </span>
                                <span className="text-xs text-on-surface-variant">
                  Created {formatMonthDay(session.created_at)}
                </span>
                            </div>
                            <SessionActions
                                slug={session.slug}
                                pinned={session.pinned}
                                archived={Boolean(session.archived_at)}
                            />
                        </div>
                        <div>
                            <SessionTitleEditor slug={session.slug} title={session.title || "Untitled session"}/>
                        </div>
                    </div>
                </header>

                {turns.length === 0 ? (
                    <section
                        className="rounded-lg border border-dashed border-outline-variant/70 bg-surface-container-low p-8 text-center">
                        <div
                            className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-lg bg-primary-fixed text-primary">
                            <Sparkles className="h-6 w-6"/>
                        </div>
                        <h2 className="text-headline-md font-headline-md text-on-surface">
                            No turns saved yet
                        </h2>
                        <p className="mx-auto mt-2 max-w-[560px] text-ui-medium leading-relaxed text-on-surface-variant">
                            Continue this session in Ask Memento to add a source-backed answer.
                        </p>
                    </section>
                ) : (
                    <section className="space-y-5">
                        {turns.map((turn) => (
                            <article
                                key={turn.id}
                                className="overflow-hidden rounded-lg border border-outline-variant/50 bg-surface-container-low shadow-sm"
                            >
                                <div
                                    className="flex flex-wrap items-start justify-between gap-3 border-b border-outline-variant/40 bg-surface-container-low px-4 py-3 sm:px-5">
                                    <div className="min-w-0">
                                        <div className="text-label-caps font-label-caps text-on-surface-variant">
                                            Turn {turn.turn_index + 1}
                                        </div>
                                        <p className="mt-1 max-w-[820px] whitespace-pre-wrap text-sm leading-6 text-on-surface">
                                            {turn.user_message}
                                        </p>
                                    </div>
                                    <div className="flex flex-wrap items-center gap-2">
                    <span
                        className={`rounded-full border px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.06em] ${statusClasses(turn.status)}`}>
                      {turn.status}
                    </span>
                                        {turn.run_id ? (
                                            <Link
                                                href="/debug"
                                                className="inline-flex items-center gap-1 rounded border border-outline-variant/60 bg-background px-2 py-1 text-[11px] font-semibold text-on-surface-variant transition hover:text-primary"
                                                title={`Debug run ${turn.run_id}${turn.run_status ? ` (${turn.run_status})` : ""}`}
                                            >
                                                Run #{turn.run_id}
                                                <ExternalLink className="h-3 w-3"/>
                                            </Link>
                                        ) : null}
                                    </div>
                                </div>

                                {turn.context_refs.length > 0 && (
                                    <div
                                        className="border-b border-outline-variant/35 bg-primary-fixed/25 px-4 py-3 sm:px-5">
                                        <div className="flex flex-wrap items-center gap-2">
                      <span className="text-[11px] font-bold uppercase tracking-[0.08em] text-primary">
                        Context
                      </span>
                                            {turn.context_refs.map((ref) => {
                                                const href = refHref(ref);
                                                const chip = (
                                                    <span
                                                        className="rounded-full border border-primary/20 bg-background px-2.5 py-1 text-xs font-medium text-on-surface">
                            <span className="font-mono text-primary">{refPrefix(ref.ref_kind)}</span>
                                                        {ref.label}
                                                        {" "}
                                                        <span
                                                            className="ml-1 text-[10px] uppercase tracking-[0.06em] text-on-surface-variant">
                              {refKindLabel(ref.ref_kind)}
                            </span>
                          </span>
                                                );
                                                return href ? (
                                                    <Link key={ref.id} href={href}
                                                          className="transition hover:opacity-80">
                                                        {chip}
                                                    </Link>
                                                ) : (
                                                    <span key={ref.id}>{chip}</span>
                                                );
                                            })}
                                        </div>
                                    </div>
                                )}

                                <div className="space-y-4 px-4 py-4 sm:px-5">
                                    {turn.assistant_answer ? (
                                        <MarkdownText text={cleanAnswer(turn.assistant_answer)}/>
                                    ) : (
                                        <div
                                            className="rounded border border-outline-variant/60 bg-background px-3 py-2 text-sm text-on-surface-variant">
                                            {turn.status === "running"
                                                ? "This answer is still running. Reload after the agent completes."
                                                : "No answer text was saved for this turn."}
                                        </div>
                                    )}

                                    {turn.cited_message_ids.length > 0 && (
                                        <div
                                            className="flex flex-wrap items-center gap-2 border-t border-outline-variant/35 pt-3">
                      <span className="text-[11px] font-bold uppercase tracking-[0.08em] text-on-surface-variant">
                        Cited messages
                      </span>
                                            {turn.cited_message_ids.map((id) => (
                                                <span
                                                    key={id}
                                                    className="rounded border border-outline-variant/60 bg-background px-2 py-1 font-mono text-[11px] text-on-surface-variant"
                                                >
                          msg:{id}
                        </span>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            </article>
                        ))}
                    </section>
                )}
            </div>
        </main>
    );
}
