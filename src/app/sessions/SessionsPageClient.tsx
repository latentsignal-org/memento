"use client";

import {useEffect, useState} from "react";
import Link from "next/link";
import {MessageSquare, Pin, Search, Sparkles} from "lucide-react";
import {apiGet} from "@/lib/api";
import {formatMonthDay, relativeDate} from "@/lib/date-utils";

interface AskSessionSummary {
    id: number;
    slug: string;
    title: string;
    summary: string;
    status: string;
    pinned: boolean;
    archived_at?: string;
    created_at: string;
    updated_at: string;
    turn_count?: number;
}

async function loadSessions(): Promise<AskSessionSummary[]> {
    const data = await apiGet<{ sessions?: AskSessionSummary[] }>("/api/sessions");
    return data?.sessions || [];
}

function compactSummary(session: AskSessionSummary): string {
    const summary = session.summary?.trim();
    if (summary) {
        return summary
            .replace(/^#{1,6}\s+/gm, "")
            .replace(/\*\*([^*]+)\*\*/g, "$1")
            .replace(/[_`>]/g, "")
            .replace(/\s+/g, " ")
            .trim();
    }
    if (session.turn_count && session.turn_count > 0) {
        return "This investigation has saved turns but no completed answer summary yet.";
    }
    return "Start asking in this session to build a source-backed investigation trail.";
}

export default function SessionsPageClient() {
    const [sessions, setSessions] = useState<AskSessionSummary[] | null>(null);

    useEffect(() => {
        let cancelled = false;
        loadSessions().then((result) => {
            if (!cancelled) setSessions(result);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (sessions === null) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    const activeCount = sessions.filter((session) => !session.archived_at).length;
    const turnCount = sessions.reduce((sum, session) => sum + (session.turn_count || 0), 0);

    return (
        <main className="min-h-screen bg-background pt-16 text-on-surface">
            <div className="mx-auto w-full max-w-[1180px] px-4 py-8 sm:px-6 sm:py-12">
                <header
                    className="flex flex-col gap-5 border-b border-outline-variant pb-8 sm:flex-row sm:items-end sm:justify-between">
                    <div className="max-w-[760px] space-y-3">
                        <Link
                            href="/home"
                            className="inline-flex items-center gap-2 text-sm font-medium text-on-surface-variant transition hover:text-primary"
                        >
                            <Search className="h-4 w-4"/>
                            Ask Memento
                        </Link>
                        <div>
                            <h1 className="text-display-lg font-display-lg text-primary tracking-tight max-sm:text-[36px]">
                                Sessions
                            </h1>
                            <p className="mt-2 max-w-[680px] text-ui-medium leading-relaxed text-on-surface-variant">
                                Saved Ask Memento investigations. Continue a thread, cite a prior session with
                                {" "}
                                <span className="font-mono text-primary">#</span>
                                {" "}
                                in a new question, or review what context was loaded for each answer.
                            </p>
                        </div>
                    </div>
                    <div className="flex flex-wrap gap-2 sm:justify-end">
            <span
                className="rounded border border-outline-variant/60 bg-surface-container-low px-3 py-1.5 font-mono text-[11px] font-bold text-on-surface-variant">
              {activeCount} ACTIVE
            </span>
                        <span
                            className="rounded border border-outline-variant/60 bg-primary-fixed px-3 py-1.5 font-mono text-[11px] font-bold text-on-primary-fixed-variant">
              {turnCount} TURNS
            </span>
                    </div>
                </header>

                {sessions.length === 0 ? (
                    <section
                        className="mt-10 rounded-lg border border-dashed border-outline-variant/70 bg-surface-container-low p-8 text-center">
                        <div
                            className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-lg bg-primary-fixed text-primary">
                            <MessageSquare className="h-6 w-6"/>
                        </div>
                        <h2 className="text-headline-md font-headline-md text-on-surface">
                            No sessions yet
                        </h2>
                        <p className="mx-auto mt-2 max-w-[560px] text-ui-medium leading-relaxed text-on-surface-variant">
                            Ask Memento from Home to create the first saved investigation. Completed answers will
                            appear here with their source and context trail.
                        </p>
                        <Link
                            href="/home"
                            className="mt-5 inline-flex items-center gap-2 rounded bg-primary px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:opacity-90"
                        >
                            <Sparkles className="h-4 w-4"/>
                            Start from Home
                        </Link>
                    </section>
                ) : (
                    <section className="mt-8 grid grid-cols-1 gap-6 md:grid-cols-2">
                        {sessions.map((session) => (
                            <Link
                                key={session.id}
                                href={`/sessions/${session.slug}`}
                                className="group relative flex min-h-[230px] flex-col justify-between rounded-2xl border border-outline-variant/40 bg-surface-container-low p-6 transition-all duration-300 hover:-translate-y-1 hover:border-outline-variant hover:bg-white hover:shadow-md"
                            >
                                <div>
                                    <div className="mb-4 flex items-center justify-between gap-3">
                                        <div className="flex min-w-0 flex-wrap items-center gap-2">
                                            {session.pinned && (
                                                <span
                                                    className="inline-flex items-center gap-1 rounded border border-primary/20 bg-primary-fixed px-2.5 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider text-primary">
                        <Pin className="h-3 w-3"/>
                        Pinned
                      </span>
                                            )}
                                            <span
                                                className="rounded border border-outline-variant/60 bg-background px-2.5 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider text-on-surface-variant">
                      {session.status || "active"}
                    </span>
                                        </div>
                                        <span className="shrink-0 text-[11px] font-medium text-on-surface-variant">
                      Updated {relativeDate(session.updated_at)}
                    </span>
                                    </div>

                                    <h2 className="mb-2 line-clamp-2 text-headline-md font-headline-md font-bold text-primary transition-colors group-hover:text-primary-container">
                                        {session.title || "Untitled session"}
                                    </h2>
                                    <p className="mb-6 line-clamp-3 text-ui-medium leading-relaxed text-on-surface-variant">
                                        {compactSummary(session)}
                                    </p>
                                </div>

                                <div
                                    className="mt-auto flex items-center justify-between border-t border-outline-variant/40 pt-4">
                  <span
                      className="rounded bg-background px-2.5 py-1 font-mono text-[11px] font-bold text-on-surface-variant">
                    {session.turn_count || 0} {(session.turn_count || 0) === 1 ? "turn" : "turns"}
                  </span>
                                    <span className="text-xs text-on-surface-variant">
                    Created {formatMonthDay(session.created_at)}
                  </span>
                                </div>
                            </Link>
                        ))}
                    </section>
                )}
            </div>
        </main>
    );
}
