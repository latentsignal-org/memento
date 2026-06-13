"use client";
import {Suspense, useCallback, useEffect, useRef, useState} from "react";
import Link from "next/link";
import {useRouter, useSearchParams} from "next/navigation";
import {ArrowLeft} from "lucide-react";
import AgentChat, {type ChatTurn} from "@/components/agent/AgentChat";
import EntityCuration, {type EntityBundle,} from "@/components/agent/EntityCuration";
import BackfillCard from "@/components/agent/BackfillCard";
import {useAgentStream} from "@/components/agent/useAgentStream";

interface DraftRow {
    id: number;
    kind: "project" | "concept";
    transcript_json: string;
    entities_json: string;
    interaction_id: string;
    status: string;
}

type DraftTranscriptLine = {
    role?: "user" | "assistant";
    text?: string;
    content?: string;
};

function parseDraftIDParam(value: string | null): number | null {
    if (!value) return null;
    const parsedId = Number.parseInt(value, 10);
    return Number.isFinite(parsedId) && parsedId > 0 ? parsedId : null;
}

function parseRunIDParam(value: string | null): number | null {
    if (!value) return null;
    const parsedId = Number.parseInt(value, 10);
    return Number.isFinite(parsedId) && parsedId > 0 ? parsedId : null;
}

function NewConceptContent() {
    const router = useRouter();
    const searchParams = useSearchParams();
    const urlDraftId = searchParams.get("draftId");
    const urlRunId = searchParams.get("runId");
    const urlDraftIDNumber = parseDraftIDParam(urlDraftId);
    const urlRunIDNumber = parseRunIDParam(urlRunId);
    const [draftId, setDraftId] = useState<number | null>(() => urlDraftIDNumber);
    const [history, setHistory] = useState<ChatTurn[]>([]);
    const [bundle, setBundle] = useState<EntityBundle | null>(null);
    const [committing, setCommitting] = useState(false);
    const [createError, setCreateError] = useState<string | null>(null);
    const [autosaveError, setAutosaveError] = useState<string | null>(null);
    const resumedRunIdRef = useRef<number | null>(null);

    const rememberRun = useCallback(
        (runId: number) => {
            const params = new URLSearchParams(searchParams.toString());
            if (draftId) params.set("draftId", String(draftId));
            params.set("runId", String(runId));
            window.history.replaceState(null, "", `?${params.toString()}`);
        },
        [draftId, searchParams],
    );
    const {events, isRunning, error, run, resume, reset} = useAgentStream(
        undefined,
        rememberRun,
    );
    const backfillProposals = events.filter(
        (e): e is Extract<(typeof events)[number], { type: "proposed_backfill" }> =>
            e.type === "proposed_backfill",
    );

    // Create a draft on first render if not provided via URL.
    useEffect(() => {
        if (urlDraftIDNumber) return;
        let cancelled = false;
        (async () => {
            try {
                const r = await fetch("/api/drafts", {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({kind: "concept"}),
                });
                if (!r.ok) throw new Error(`create draft: ${r.status}`);
                const {id} = (await r.json()) as { id: number };
                if (!cancelled) setDraftId(id);
            } catch (e) {
                if (!cancelled) setCreateError((e as Error).message);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [urlDraftIDNumber]);

    // Load draft data when draftId is set.
    useEffect(() => {
        if (!draftId) return;
        (async () => {
            try {
                const r = await fetch(`/api/drafts/${draftId}`, {cache: "no-store"});
                if (!r.ok) return;
                const d = (await r.json()) as DraftRow;
                try {
                    const tx = JSON.parse(d.transcript_json) as DraftTranscriptLine[];
                    if (Array.isArray(tx)) {
                        setHistory(
                            tx
                                .filter(
                                    (line) => line.role === "user" || line.role === "assistant",
                                )
                                .map((line) => ({
                                    role: line.role as "user" | "assistant",
                                    text: line.text ?? line.content ?? "",
                                })),
                        );
                    }
                } catch {
                    /* ignore */
                }
                try {
                    const b = JSON.parse(d.entities_json) as EntityBundle;
                    if (b && b.name !== undefined) setBundle(b);
                } catch {
                    /* ignore */
                }
            } catch (e) {
                console.error("Failed to load draft on mount:", e);
            }
        })();
    }, [draftId]);

    useEffect(() => {
        if (
            !urlRunIDNumber ||
            resumedRunIdRef.current === urlRunIDNumber ||
            isRunning ||
            events.length > 0
        ) {
            return;
        }
        resumedRunIdRef.current = urlRunIDNumber;
        resume(`/api/agents/runs/${urlRunIDNumber}/events`);
    }, [events.length, isRunning, resume, urlRunIDNumber]);

    // When the agent run ends, refresh the draft.
    useEffect(() => {
        if (!draftId || isRunning) return;
        const done = events.find((e) => e.type === "done");
        if (!done) return;
        (async () => {
            const r = await fetch(`/api/drafts/${draftId}`, {cache: "no-store"});
            if (!r.ok) return;
            const d = (await r.json()) as DraftRow;
            try {
                const tx = JSON.parse(d.transcript_json) as DraftTranscriptLine[];
                if (Array.isArray(tx)) {
                    setHistory(
                        tx
                            .filter(
                                (line) => line.role === "user" || line.role === "assistant",
                            )
                            .map((line) => ({
                                role: line.role as "user" | "assistant",
                                text: line.text ?? line.content ?? "",
                            })),
                    );
                }
            } catch {
                /* ignore */
            }
            try {
                const b = JSON.parse(d.entities_json) as EntityBundle;
                if (b && b.name !== undefined) setBundle(b);
            } catch {
                /* ignore */
            }
            reset();
            const params = new URLSearchParams(searchParams.toString());
            params.delete("runId");
            window.history.replaceState(null, "", `?${params.toString()}`);
        })();
    }, [events, isRunning, draftId, reset, searchParams]);

    const handleSend = useCallback(
        (message: string) => {
            if (!draftId) return;
            setHistory((prev) => [...prev, {role: "user", text: message}]);
            run(`/api/drafts/${draftId}/turn`, {message});
        },
        [draftId, run],
    );

    const handleBundleChange = useCallback(
        async (next: EntityBundle) => {
            if (!draftId) return;
            try {
                const r = await fetch(`/api/drafts/${draftId}/entities`, {
                    method: "PATCH",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify(next),
                });
                if (!r.ok) {
                    throw new Error(`update draft entities: ${r.status}`);
                }
                setAutosaveError(null);
            } catch (e) {
                setAutosaveError((e as Error).message);
            }
        },
        [draftId],
    );

    const handleCommit = useCallback(async () => {
        if (!draftId) return;
        setCommitting(true);
        try {
            const r = await fetch(`/api/drafts/${draftId}/commit`, {
                method: "POST",
            });
            if (!r.ok) {
                const text = await r.text();
                alert(`Commit failed: ${text}`);
                return;
            }
            const {slug} = (await r.json()) as { slug: string };
            router.push(`/concepts/${slug}`);
        } finally {
            setCommitting(false);
        }
    }, [draftId, router]);

    if (createError) {
        return (
            <main className="pt-16 min-h-screen bg-background text-on-surface p-8">
                <div className="text-red-600">
                    Could not create draft: {createError}
                </div>
            </main>
        );
    }

    return (
        <main className="min-h-screen bg-background pt-16 text-on-surface lg:h-[100dvh] lg:overflow-hidden">
            <div className="mx-auto flex w-full max-w-[1440px] flex-col gap-5 px-4 sm:px-6 py-8 lg:h-full lg:min-h-0">
                <header className="flex flex-wrap items-end justify-between gap-4">
                    <div className="min-w-0 space-y-3">
                        <Link
                            href="/concepts"
                            className="inline-flex items-center gap-2 text-sm font-medium text-on-surface-variant transition hover:text-primary"
                        >
                            <ArrowLeft className="h-4 w-4"/>
                            Concepts
                        </Link>
                        <div className="space-y-2">
                            <div className="flex flex-wrap items-center gap-3">
                                <h1 className="text-display-lg font-display-lg text-primary tracking-tight max-sm:text-[32px]">
                                    New concept
                                </h1>
                                <span
                                    className="inline-flex items-center gap-1.5 rounded border border-outline-variant/60 bg-primary-fixed px-2.5 py-1 text-[11px] font-semibold text-on-primary-fixed-variant">
                  Collector agent
                </span>
                            </div>
                            <p className="max-w-[760px] text-ui-medium text-on-surface-variant">
                                Declare an evergreen topic, let the agent find supporting
                                sources, then curate the messages and threads that should ground
                                it.
                            </p>
                        </div>
                    </div>
                </header>

                <div className="grid min-h-[560px] grid-cols-1 gap-5 lg:min-h-0 lg:flex-1 lg:grid-cols-12">
                    {/* On mobile the stacked chat pane needs its own height; on lg it stretches with the grid row */}
                    <div className="h-[60dvh] min-h-[380px] lg:h-auto lg:min-h-0 lg:col-span-5">
                        <AgentChat
                            history={history}
                            liveEvents={events}
                            isRunning={isRunning}
                            error={error}
                            onSend={handleSend}
                            disabled={!draftId}
                            submitLabel="Collect"
                            submittingLabel="Collecting"
                            placeholder={
                                draftId
                                    ? "e.g. 'Create a concept for EB5 investment patterns.'"
                                    : "Setting up draft..."
                            }
                            enableMentions
                        />
                    </div>
                    <div className="min-h-0 lg:col-span-7 flex flex-col gap-3">
                        {(backfillProposals.length > 0 || isRunning) && (
                            <div className="space-y-2 shrink-0 overflow-y-auto lg:max-h-[40%] pr-1">
                                {backfillProposals.map((e, i) => (
                                    <BackfillCard
                                        key={i}
                                        backfillUrl={`/api/drafts/${draftId}/backfill`}
                                        decisionId={e.decision_id}
                                        rationale={e.rationale}
                                        candidateMessageIds={e.candidate_message_ids}
                                        gapKind={e.gap_kind}
                                    />
                                ))}
                                {isRunning && (
                                    <div className="flex items-center gap-2 px-1 text-xs text-on-surface-variant">
                                        <span
                                            className="inline-block h-1.5 w-1.5 rounded-full bg-primary animate-pulse"/>
                                        Agent is working…
                                    </div>
                                )}
                            </div>
                        )}
                        <div className="min-h-0 flex-1">
                            <EntityCuration
                                bundle={bundle}
                                onChange={handleBundleChange}
                                onCommit={handleCommit}
                                committing={committing}
                                entityLabel="Concept"
                                saveLabel="Save concept"
                                emptyTitle="Concept source bundle"
                                emptyDescription="Proposed people, messages, and threads will appear here as candidate evidence."
                                sessionType="collector"
                                entityId={draftId ? String(draftId) : undefined}
                            />
                        </div>
                        {autosaveError && (
                            <div className="rounded border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
                                Draft autosave failed: {autosaveError}
                            </div>
                        )}
                    </div>
                </div>
            </div>
        </main>
    );
}

export default function NewConceptPage() {
    return (
        <Suspense
            fallback={
                <div className="pt-16 min-h-screen bg-background text-on-surface p-8">
                    Loading draft...
                </div>
            }
        >
            <NewConceptContent/>
        </Suspense>
    );
}
