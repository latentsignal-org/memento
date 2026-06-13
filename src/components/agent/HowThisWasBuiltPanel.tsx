"use client";

import {useCallback, useEffect, useMemo, useRef, useState} from "react";
import {LoaderCircle, RefreshCw, Wrench, X} from "lucide-react";
import {safePreviewJson, safePreviewText} from "./panel-safety";
import {getToolLabel} from "@/lib/tool-labels";
import {normalizeLogsResponse} from "@/lib/agent-logs";

type SessionType = "project_compile" | "concept_compile" | "person_enrich";
type ProvenanceDimension = "projects" | "concepts";

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

interface DraftRevision {
    id: number;
    draft_id: number;
    revision_kind: string;
    transcript_json: string;
    entities_json: string;
    created_at: string;
}

interface CollectorLoop {
    session_id: number;
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

interface ProvenanceResponse {
    draft_id: number;
    kind: string;
    status: string;
    committed_entity_id?: number;
    revisions: DraftRevision[];
    collector_loops: CollectorLoop[];
}

interface Props {
    sessionType: SessionType;
    entityId: string;
    provenanceDimension?: ProvenanceDimension;
    provenanceSlug?: string;
    buttonStyle?: "default" | "link";
}

interface PromptEvent {
    prompt: string;
    createdAt: string;
    revisionIndex: number;
}

interface ActivitySummary {
    startingBundleLoads: number;
    fullTextSearches: number;
    semanticSearches: number;
    scopedSearches: number;
    messagesFetched: number;
    threadsFetched: number;
    notesLoaded: number;
    profileLoads: number;
    peopleLookups: number;
    relatedPages: number;
    fullTextQueries: string[];
    semanticQueries: string[];
    scopedQueries: string[];
}

interface RevisionDelta {
    prompt: string;
    addedPeople: number;
    removedPeople: number;
    addedMessages: number;
    removedMessages: number;
    addedThreads: number;
    removedThreads: number;
    renamed: boolean;
}

export default function HowThisWasBuiltPanel({
                                                 sessionType,
                                                 entityId,
                                                 provenanceDimension,
                                                 provenanceSlug,
                                                 buttonStyle = "link",
                                             }: Props) {
    const [open, setOpen] = useState(false);
    const [logs, setLogs] = useState<AgentLoopLog[]>([]);
    const [provenance, setProvenance] = useState<ProvenanceResponse | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [showTechnicalDetails, setShowTechnicalDetails] = useState(false);
    const rootRef = useRef<HTMLDivElement | null>(null);

    const fetchData = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const logPromise = fetch(`/api/agents/logs?type=${sessionType}&entityId=${entityId}`).then(async (res) => {
                if (!res.ok) throw new Error("Unable to load build history.");
                return normalizeLogsResponse<AgentLoopLog>(await res.json()).loops;
            });

            const provenancePromise = provenanceDimension && provenanceSlug
                ? fetch(`/api/${provenanceDimension}/${provenanceSlug}/provenance`).then(async (res) => {
                    if (res.status === 404) return null;
                    if (!res.ok) throw new Error("Unable to load build history.");
                    return (await res.json()) as ProvenanceResponse;
                })
                : Promise.resolve(null);

            const [nextLogs, nextProvenance] = await Promise.all([logPromise, provenancePromise]);
            setLogs(nextLogs);
            setProvenance(nextProvenance);
        } catch (e) {
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            setLoading(false);
        }
    }, [entityId, provenanceDimension, provenanceSlug, sessionType]);

    useEffect(() => {
        if (open) fetchData();
    }, [open, fetchData]);

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

    const prompts = useMemo(() => extractPromptEvents(provenance), [provenance]);
    const refinements = useMemo(() => extractRefinements(provenance), [provenance]);
    const evidenceActivity = useMemo(
        () => summarizeActivity(provenance?.collector_loops ?? [], logs),
        [logs, provenance?.collector_loops],
    );
    const finalGeneration = useMemo(() => summarizeGeneration(logs, sessionType), [logs, sessionType]);

    const hasContent =
        logs.length > 0 ||
        (provenance?.revisions.length ?? 0) > 0 ||
        prompts.length > 0 ||
        refinements.length > 0;

    return (
        <div ref={rootRef} className="relative inline-flex justify-end">
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
                <Wrench className="h-3 w-3"/>
                {open ? "Hide Evidence Trail" : "Show Evidence Trail"}
            </button>

            {open ? (
                <div
                    className="absolute right-0 top-full z-30 mt-2 w-[min(560px,calc(100vw-2rem))] max-h-[80vh] overflow-y-auto rounded-2xl border border-outline-variant/45 bg-background shadow-lg">
                    <div
                        className="flex items-start justify-between gap-3 border-b border-outline-variant/35 px-5 py-4">
                        <div>
                            <div
                                className="text-[11px] font-semibold uppercase tracking-[0.14em] text-on-surface-variant">
                                Evidence Trail
                            </div>
                            <h3 className="mt-1 text-lg font-semibold text-primary">How This Was Built</h3>
                            <p className="mt-1 max-w-[420px] text-sm leading-relaxed text-on-surface-variant">
                                Shows the exact request, the evidence-gathering steps, any refinements, and the final
                                generation pass.
                            </p>
                        </div>
                        <div className="flex items-center gap-2">
                            <button
                                type="button"
                                onClick={fetchData}
                                className="rounded p-1 text-on-surface-variant hover:bg-surface-container"
                                title="Refresh build history"
                            >
                                <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`}/>
                            </button>
                            <button
                                type="button"
                                onClick={() => setOpen(false)}
                                className="rounded-full border border-outline-variant/60 p-1 text-on-surface-variant hover:bg-surface-container"
                                title="Close panel"
                            >
                                <X className="h-4 w-4"/>
                            </button>
                        </div>
                    </div>

                    <div className="space-y-5 px-5 py-5">
                        {loading && !hasContent ? (
                            <div className="flex items-center gap-2 text-sm text-on-surface-variant">
                                <LoaderCircle className="h-4 w-4 animate-spin"/>
                                Loading build history...
                            </div>
                        ) : error ? (
                            <div
                                className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
                        ) : !hasContent ? (
                            <div
                                className="rounded-xl border border-outline-variant/30 bg-surface-container-low px-4 py-3 text-sm text-on-surface-variant">
                                No build history is available yet for this page.
                            </div>
                        ) : (
                            <>
                                <HistorySection
                                    label="Origin"
                                    title={provenanceDimension ? "Started from your request" : "Started from the person enrichment flow"}
                                >
                                    {prompts[0] ? (
                                        <ExactPromptBlock prompt={prompts[0].prompt} label="Exact prompt"/>
                                    ) : logs[0]?.input_content ? (
                                        <ExactPromptBlock prompt={logs[0].input_content} label="Run input"/>
                                    ) : (
                                        <p className="text-sm leading-relaxed text-on-surface">
                                            This page was generated from the current entity and its attached evidence.
                                        </p>
                                    )}
                                    <p className="text-sm leading-relaxed text-on-surface-variant">
                                        {provenanceDimension
                                            ? "The system began from the saved draft flow for this entity, then gathered supporting evidence before writing the final output."
                                            : "This page was generated directly from the person detail page using the enrichment flow, without a separate draft conversation."}
                                    </p>
                                </HistorySection>

                                <HistorySection
                                    label="Evidence Gathering"
                                    title="Gathered and checked supporting evidence"
                                >
                                    <p className="text-sm leading-relaxed text-on-surface">
                                        The system searched the archive, loaded related context, and opened source
                                        records directly before writing the final page.
                                    </p>
                                    <div className="mt-3 flex flex-wrap gap-2">
                                        {evidenceActivity.startingBundleLoads > 0 ?
                                            <ActivityChip label="Starting bundle loads"
                                                          value={String(evidenceActivity.startingBundleLoads)}/> : null}
                                        {evidenceActivity.fullTextSearches > 0 ? <ActivityChip label="Full-text search"
                                                                                               value={String(evidenceActivity.fullTextSearches)}/> : null}
                                        {evidenceActivity.semanticSearches > 0 ? <ActivityChip label="Semantic search"
                                                                                               value={String(evidenceActivity.semanticSearches)}/> : null}
                                        {evidenceActivity.scopedSearches > 0 ? <ActivityChip label="Scoped search"
                                                                                             value={String(evidenceActivity.scopedSearches)}/> : null}
                                        {evidenceActivity.messagesFetched > 0 ? <ActivityChip label="Messages fetched"
                                                                                              value={String(evidenceActivity.messagesFetched)}/> : null}
                                        {evidenceActivity.threadsFetched > 0 ? <ActivityChip label="Threads fetched"
                                                                                             value={String(evidenceActivity.threadsFetched)}/> : null}
                                        {evidenceActivity.notesLoaded > 0 ? <ActivityChip label="Notes loaded"
                                                                                          value={String(evidenceActivity.notesLoaded)}/> : null}
                                        {evidenceActivity.profileLoads > 0 ? <ActivityChip label="Profile loads"
                                                                                           value={String(evidenceActivity.profileLoads)}/> : null}
                                        {evidenceActivity.peopleLookups > 0 ? <ActivityChip label="People lookups"
                                                                                            value={String(evidenceActivity.peopleLookups)}/> : null}
                                        {evidenceActivity.relatedPages > 0 ? <ActivityChip label="Related pages"
                                                                                           value={String(evidenceActivity.relatedPages)}/> : null}
                                    </div>
                                    <DetailList
                                        title="Specific search and fetch details"
                                        items={[
                                            ...evidenceActivity.fullTextQueries.slice(0, 3).map((query) => `Full-text search query: "${query}"`),
                                            ...evidenceActivity.semanticQueries.slice(0, 2).map((query) => `Semantic search topic: "${query}"`),
                                            ...evidenceActivity.scopedQueries.slice(0, 2).map((query) => `Scoped relationship query: "${query}"`),
                                            evidenceActivity.messagesFetched > 0 ? `Opened ${evidenceActivity.messagesFetched} source message${evidenceActivity.messagesFetched === 1 ? "" : "s"} directly for verification.` : "",
                                            evidenceActivity.threadsFetched > 0 ? `Opened ${evidenceActivity.threadsFetched} conversation thread${evidenceActivity.threadsFetched === 1 ? "" : "s"} for added context.` : "",
                                        ].filter(Boolean)}
                                    />
                                </HistorySection>

                                {refinements.length > 0 ? (
                                    <HistorySection
                                        label="Refinements"
                                        title="Applied changes after your feedback"
                                    >
                                        <div className="space-y-3">
                                            {refinements.map((refinement, index) => (
                                                <div key={`${refinement.prompt}-${index}`}
                                                     className="rounded-xl border border-outline-variant/30 bg-surface-container-low px-4 py-3">
                                                    <ExactPromptBlock prompt={refinement.prompt}
                                                                      label="Exact follow-up prompt" compact/>
                                                    <div className="mt-3 flex flex-wrap gap-2">
                                                        {refinement.renamed ? <ActivityChip label="Renamed" value="yes"
                                                                                            tone="amber"/> : null}
                                                        {refinement.addedPeople > 0 ? <ActivityChip label="Added people"
                                                                                                    value={String(refinement.addedPeople)}
                                                                                                    tone="green"/> : null}
                                                        {refinement.removedPeople > 0 ?
                                                            <ActivityChip label="Removed people"
                                                                          value={String(refinement.removedPeople)}
                                                                          tone="amber"/> : null}
                                                        {refinement.addedMessages > 0 ?
                                                            <ActivityChip label="Added messages"
                                                                          value={String(refinement.addedMessages)}
                                                                          tone="green"/> : null}
                                                        {refinement.removedMessages > 0 ?
                                                            <ActivityChip label="Removed messages"
                                                                          value={String(refinement.removedMessages)}
                                                                          tone="amber"/> : null}
                                                        {refinement.addedThreads > 0 ?
                                                            <ActivityChip label="Added threads"
                                                                          value={String(refinement.addedThreads)}
                                                                          tone="green"/> : null}
                                                        {refinement.removedThreads > 0 ?
                                                            <ActivityChip label="Removed threads"
                                                                          value={String(refinement.removedThreads)}
                                                                          tone="amber"/> : null}
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    </HistorySection>
                                ) : null}

                                <HistorySection
                                    label="Final Generation"
                                    title={sessionType === "person_enrich" ? "Generated the final enrichment output" : "Generated the final narrative"}
                                >
                                    <p className="text-sm leading-relaxed text-on-surface">
                                        After gathering
                                        evidence{refinements.length > 0 ? " and applying your refinements" : ""}, the
                                        system wrote the final output for this page.
                                    </p>
                                    <div className="mt-3 flex flex-wrap gap-2">
                                        {finalGeneration.sectionsWritten > 0 ? <ActivityChip label="Sections written"
                                                                                             value={String(finalGeneration.sectionsWritten)}/> : null}
                                        {finalGeneration.facetsWritten > 0 ? <ActivityChip label="Facets written"
                                                                                           value={String(finalGeneration.facetsWritten)}/> : null}
                                        {finalGeneration.toolCalls > 0 ? <ActivityChip label="Tool calls"
                                                                                       value={String(finalGeneration.toolCalls)}/> : null}
                                        {finalGeneration.durationSeconds > 0 ? <ActivityChip label="Run time"
                                                                                             value={`${finalGeneration.durationSeconds.toFixed(1)}s`}/> : null}
                                    </div>
                                    {finalGeneration.latestAssistantText ? (
                                        <div
                                            className="mt-3 rounded-xl border border-outline-variant/30 bg-surface-container-low px-4 py-3">
                                            <div
                                                className="text-[10px] font-semibold uppercase tracking-[0.14em] text-on-surface-variant">Run
                                                summary
                                            </div>
                                            <p className="mt-2 text-sm leading-relaxed text-on-surface">
                                                {safePreviewText(finalGeneration.latestAssistantText, 320)}
                                            </p>
                                        </div>
                                    ) : null}
                                </HistorySection>

                                <div
                                    className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm leading-relaxed text-emerald-800">
                                    <strong className="font-semibold">Why trust this page?</strong>{" "}
                                    The final output is tied to specific source messages, and this panel shows both the
                                    evidence-gathering path and any refinements that changed the final result.
                                </div>

                                <div className="space-y-2">
                                    <button
                                        type="button"
                                        onClick={() => setShowTechnicalDetails((v) => !v)}
                                        className="text-xs font-semibold text-primary hover:underline"
                                    >
                                        {showTechnicalDetails ? "Hide technical details" : "Show technical details"}
                                    </button>
                                    {showTechnicalDetails ? (
                                        <div
                                            className="rounded-xl border border-outline-variant/30 bg-surface-container-low px-4 py-3">
                                            <DetailList
                                                title="Raw tool activity"
                                                items={buildTechnicalDetails(provenance?.collector_loops ?? [], logs)}
                                                emptyLabel="No additional tool activity recorded."
                                            />
                                        </div>
                                    ) : null}
                                </div>
                            </>
                        )}
                    </div>
                </div>
            ) : null}
        </div>
    );
}

function HistorySection({
                            label,
                            title,
                            children,
                        }: {
    label: string;
    title: string;
    children: React.ReactNode;
}) {
    return (
        <section
            className="grid gap-3 border-t border-outline-variant/30 pt-5 first:border-t-0 first:pt-0 md:grid-cols-[110px_minmax(0,1fr)] md:gap-5">
            <div
                className="pt-1 text-[11px] font-semibold uppercase tracking-[0.16em] text-on-surface-variant">{label}</div>
            <div>
                <h4 className="text-base font-semibold text-primary">{title}</h4>
                <div className="mt-2 space-y-3">{children}</div>
            </div>
        </section>
    );
}

function ExactPromptBlock({prompt, label, compact = false}: { prompt: string; label: string; compact?: boolean }) {
    return (
        <div
            className={`rounded-xl border border-outline-variant/30 bg-surface-container-low ${compact ? "px-3 py-3" : "px-4 py-3"}`}>
            <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-on-surface-variant">{label}</div>
            <p className="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-on-surface">“{safePreviewText(prompt, compact ? 240 : 360)}”</p>
        </div>
    );
}

function ActivityChip({
                          label,
                          value,
                          tone = "default",
                      }: {
    label: string;
    value: string;
    tone?: "default" | "green" | "amber";
}) {
    const toneClass =
        tone === "green"
            ? "border-emerald-200 bg-emerald-50 text-emerald-700"
            : tone === "amber"
                ? "border-amber-200 bg-amber-50 text-amber-700"
                : "border-outline-variant/40 bg-background text-on-surface";

    return (
        <span className={`rounded-full border px-2.5 py-1 text-[12px] ${toneClass}`}>
      <span className="font-semibold">{label}</span>: {value}
    </span>
    );
}

function DetailList({title, items, emptyLabel}: { title: string; items: string[]; emptyLabel?: string }) {
    const filtered = items.filter(Boolean);
    return (
        <div className="mt-3">
            <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-on-surface-variant">{title}</div>
            {filtered.length > 0 ? (
                <ul className="mt-2 space-y-1 text-sm leading-relaxed text-on-surface-variant">
                    {filtered.map((item, index) => (
                        <li key={`${item}-${index}`}>- {item}</li>
                    ))}
                </ul>
            ) : emptyLabel ? (
                <p className="mt-2 text-sm text-on-surface-variant">{emptyLabel}</p>
            ) : null}
        </div>
    );
}

function extractPromptEvents(provenance: ProvenanceResponse | null): PromptEvent[] {
    if (!provenance) return [];
    const prompts: PromptEvent[] = [];
    for (let index = 0; index < provenance.revisions.length; index += 1) {
        const revision = provenance.revisions[index];
        try {
            const transcript = JSON.parse(revision.transcript_json);
            if (!Array.isArray(transcript)) continue;
            for (let i = transcript.length - 1; i >= 0; i -= 1) {
                const item = transcript[i];
                if (item && item.role === "user" && typeof item.text === "string" && item.text.trim()) {
                    const prompt = item.text.trim();
                    if (!prompts.some((existing) => existing.prompt === prompt)) {
                        prompts.push({prompt, createdAt: revision.created_at, revisionIndex: index});
                    }
                    break;
                }
            }
        } catch {
            // ignore malformed transcript snapshot
        }
    }
    return prompts;
}

function extractRefinements(provenance: ProvenanceResponse | null): RevisionDelta[] {
    if (!provenance || provenance.revisions.length < 2) return [];
    const prompts = extractPromptEvents(provenance);
    const refinements: RevisionDelta[] = [];

    for (let index = 1; index < provenance.revisions.length; index += 1) {
        const current = provenance.revisions[index];
        const previous = provenance.revisions[index - 1];
        const promptEvent = prompts.find((event) => event.revisionIndex === index);
        const diff = diffRevision(previous, current);
        const hasChange =
            diff.renamed ||
            diff.addedPeople.length > 0 ||
            diff.removedPeople.length > 0 ||
            diff.addedMessages.length > 0 ||
            diff.removedMessages.length > 0 ||
            diff.addedThreads.length > 0 ||
            diff.removedThreads.length > 0;

        if (!promptEvent && !hasChange) continue;

        refinements.push({
            prompt: promptEvent?.prompt ?? "The bundle was revised based on additional system context.",
            addedPeople: diff.addedPeople.length,
            removedPeople: diff.removedPeople.length,
            addedMessages: diff.addedMessages.length,
            removedMessages: diff.removedMessages.length,
            addedThreads: diff.addedThreads.length,
            removedThreads: diff.removedThreads.length,
            renamed: diff.renamed,
        });
    }

    return refinements;
}

function summarizeActivity(collectorLoops: CollectorLoop[], logs: AgentLoopLog[]): ActivitySummary {
    const summary: ActivitySummary = {
        startingBundleLoads: 0,
        fullTextSearches: 0,
        semanticSearches: 0,
        scopedSearches: 0,
        messagesFetched: 0,
        threadsFetched: 0,
        notesLoaded: 0,
        profileLoads: 0,
        peopleLookups: 0,
        relatedPages: 0,
        fullTextQueries: [],
        semanticQueries: [],
        scopedQueries: [],
    };

    for (const activity of [...collectorLoops, ...logs]) {
        let toolCalls: Array<{ name?: string; args?: Record<string, unknown> }> = [];
        try {
            const parsed = JSON.parse(activity.tool_calls_json || "[]");
            toolCalls = Array.isArray(parsed) ? parsed : [];
        } catch {
            toolCalls = [];
        }

        for (const call of toolCalls) {
            const name = call?.name ?? "";
            const args = call?.args ?? {};
            switch (name) {
                case "get_project_bundle":
                case "get_concept_bundle":
                    summary.startingBundleLoads += 1;
                    break;
                case "fts_search":
                    summary.fullTextSearches += 1;
                    if (typeof args.query === "string") summary.fullTextQueries.push(args.query);
                    break;
                case "fts_search_scoped":
                    summary.scopedSearches += 1;
                    if (typeof args.query === "string") summary.scopedQueries.push(args.query);
                    break;
                case "vector_search":
                    summary.semanticSearches += 1;
                    if (typeof args.query === "string") summary.semanticQueries.push(args.query);
                    break;
                case "get_message":
                    summary.messagesFetched += 1;
                    break;
                case "get_thread":
                    summary.threadsFetched += 1;
                    break;
                case "get_notes":
                    summary.notesLoaded += 1;
                    break;
                case "find_people":
                    summary.peopleLookups += 1;
                    break;
                case "get_person_summary":
                    summary.profileLoads += 1;
                    summary.relatedPages += 1;
                    break;
                default:
                    break;
            }
        }
    }

    summary.fullTextQueries = unique(summary.fullTextQueries).slice(0, 5);
    summary.semanticQueries = unique(summary.semanticQueries).slice(0, 5);
    summary.scopedQueries = unique(summary.scopedQueries).slice(0, 5);
    return summary;
}

function summarizeGeneration(logs: AgentLoopLog[], sessionType: SessionType) {
    const toolCalls = logs.flatMap((log) => {
        try {
            const parsed = JSON.parse(log.tool_calls_json || "[]");
            return Array.isArray(parsed) ? parsed : [];
        } catch {
            return [];
        }
    });
    const totalDurationMs = logs.reduce((sum, log) => sum + (log.duration_ms || 0), 0);
    const latestAssistantText = [...logs].reverse().find((log) => log.assistant_text)?.assistant_text ?? "";
    return {
        sectionsWritten: logs.flatMap((log) => {
            try {
                const parsed = JSON.parse(log.tool_results_json || "[]");
                return Array.isArray(parsed) ? parsed : [];
            } catch {
                return [];
            }
        }).filter((result: any) => result?.name === (sessionType === "concept_compile" ? "write_concept_section" : sessionType === "project_compile" ? "write_section" : "write_person_section")).length,
        facetsWritten: logs.flatMap((log) => {
            try {
                const parsed = JSON.parse(log.tool_results_json || "[]");
                return Array.isArray(parsed) ? parsed : [];
            } catch {
                return [];
            }
        }).filter((result: any) => result?.name === "write_facet").length,
        toolCalls: toolCalls.length,
        durationSeconds: totalDurationMs / 1000,
        latestAssistantText,
    };
}

function buildTechnicalDetails(collectorLoops: CollectorLoop[], logs: AgentLoopLog[]): string[] {
    const lines: string[] = [];
    for (const activity of [...collectorLoops, ...logs]) {
        try {
            const toolCalls = JSON.parse(activity.tool_calls_json || "[]");
            for (const call of Array.isArray(toolCalls) ? toolCalls : []) {
                const name = typeof call?.name === "string" ? call.name : "tool";
                const args = call?.args ?? {};
                lines.push(`${getToolLabel(name, args)}: ${safePreviewJson(args, 200)}`);
            }
        } catch {
            // ignore malformed tool data
        }
    }
    return lines.slice(0, 24);
}

function parseEntities(entitiesJSON: string) {
    try {
        const entities = JSON.parse(entitiesJSON);
        return {
            name: typeof entities?.name === "string" ? entities.name : "",
            people: Array.isArray(entities?.people) ? entities.people : [],
            messages: Array.isArray(entities?.messages) ? entities.messages : [],
            threads: Array.isArray(entities?.threads) ? entities.threads : [],
        };
    } catch {
        return {name: "", people: [], messages: [], threads: []};
    }
}

function diffRevision(previousRevision: DraftRevision | null, currentRevision: DraftRevision) {
    const prev = previousRevision ? parseEntities(previousRevision.entities_json) : {
        name: "",
        people: [],
        messages: [],
        threads: []
    };
    const curr = parseEntities(currentRevision.entities_json);

    const prevPeople = new Set(prev.people.map((p: any) => Number(p.person_id)));
    const currPeople = new Set(curr.people.map((p: any) => Number(p.person_id)));
    const prevMessages = new Set(prev.messages.map((m: any) => Number(m.message_id)));
    const currMessages = new Set(curr.messages.map((m: any) => Number(m.message_id)));
    const prevThreads = new Set(prev.threads.map((t: any) => Number(t.thread_id)));
    const currThreads = new Set(curr.threads.map((t: any) => Number(t.thread_id)));

    return {
        renamed: prev.name !== curr.name && Boolean(prev.name || curr.name),
        addedPeople: [...currPeople].filter((id) => !prevPeople.has(id)),
        removedPeople: [...prevPeople].filter((id) => !currPeople.has(id)),
        addedMessages: [...currMessages].filter((id) => !prevMessages.has(id)),
        removedMessages: [...prevMessages].filter((id) => !currMessages.has(id)),
        addedThreads: [...currThreads].filter((id) => !prevThreads.has(id)),
        removedThreads: [...prevThreads].filter((id) => !currThreads.has(id)),
    };
}

function unique(values: string[]) {
    return Array.from(new Set(values.map((value) => safePreviewText(value, 160))));
}
