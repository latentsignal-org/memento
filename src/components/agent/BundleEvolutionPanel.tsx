"use client";

import {useCallback, useEffect, useMemo, useRef, useState} from "react";
import {ChevronDown, GitBranch, LoaderCircle, RefreshCw} from "lucide-react";
import {safePreviewJson, safePreviewText} from "./panel-safety";
import {getToolLabel} from "@/lib/tool-labels";

interface DraftRevision {
    id: number;
    draft_id: number;
    revision_kind: string;
    transcript_json: string;
    entities_json: string;
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

interface Props {
    dimension: "projects" | "concepts";
    slug: string;
    buttonStyle?: "default" | "link";
}

export default function BundleEvolutionPanel({dimension, slug, buttonStyle = "default"}: Props) {
    const [open, setOpen] = useState(false);
    const [data, setData] = useState<ProvenanceResponse | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const rootRef = useRef<HTMLDivElement | null>(null);

    const fetchData = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const res = await fetch(`/api/${dimension}/${slug}/provenance`);
            if (res.status === 404) {
                setData({
                    draft_id: 0,
                    kind: "",
                    status: "",
                    revisions: [],
                    collector_loops: [],
                });
                return;
            }
            if (!res.ok) throw new Error("Unable to load evolution history.");
            setData(await res.json());
        } catch (e) {
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            setLoading(false);
        }
    }, [dimension, slug]);

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
                    <GitBranch className="h-3 w-3"/>
                    {open ? "Hide Evolution" : "Show Evolution"}
                </button>
                {open ? (
                    <button onClick={fetchData}
                            className="p-1 rounded hover:bg-outline-variant/30 text-on-surface-variant"
                            title="Refresh evolution">
                        <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`}/>
                    </button>
                ) : null}
            </div>
            {open ? (
                <div
                    className="absolute right-0 top-full z-30 mt-2 w-[360px] max-w-[min(360px,calc(100vw-2rem))] max-h-72 overflow-y-auto rounded border border-outline-variant/40 bg-surface-container-low p-3 space-y-3 text-xs shadow-lg">
                    {loading && !data ? (
                        <div className="text-on-surface-variant flex items-center gap-2"><LoaderCircle
                            className="h-4 w-4 animate-spin"/>Loading evolution history...</div>
                    ) : error ? (
                        <div className="text-red-600">{error}</div>
                    ) : !data || data.revisions.length === 0 ? (
                        <div className="text-on-surface-variant">No evolution history available for this entity.</div>
                    ) : (
                        data.revisions.map((revision, index) => (
                            <RevisionCard
                                key={revision.id}
                                revision={revision}
                                previousRevision={index > 0 ? data.revisions[index - 1] : null}
                                loops={loopsForRevision(data.collector_loops, data.revisions, index)}
                                index={index}
                                isLatest={index === data.revisions.length - 1}
                            />
                        ))
                    )}
                </div>
            ) : null}
        </div>
    );
}

function RevisionCard({
                          revision,
                          previousRevision,
                          loops,
                          index,
                          isLatest,
                      }: {
    revision: DraftRevision;
    previousRevision: DraftRevision | null;
    loops: CollectorLoop[];
    index: number;
    isLatest: boolean;
}) {
    const [showRaw, setShowRaw] = useState(false);
    const [showActivity, setShowActivity] = useState(false);
    const summary = useMemo(() => summarizeRevision(revision), [revision]);
    const diff = useMemo(() => diffRevision(previousRevision, revision), [previousRevision, revision]);
    const activity = useMemo(() => summarizeCollectorLoops(loops), [loops]);

    return (
        <div className="rounded border border-outline-variant/40 bg-surface-container-lowest p-2.5 space-y-2">
            <div className="flex items-center justify-between gap-2">
        <span className="font-semibold text-on-surface bg-primary/10 text-primary px-1.5 py-0.5 rounded text-[10px]">
          Revision {index + 1}
        </span>
                <span className="text-[10px] text-on-surface-variant">
          {isLatest ? "Committed snapshot" : revision.revision_kind}
        </span>
            </div>
            {summary.prompt ? (
                <div>
                    <div className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Prompt
                    </div>
                    <div
                        className="mt-1 rounded bg-surface-container-low px-2 py-1 text-[11px] text-on-surface-variant leading-relaxed">
                        {safePreviewText(summary.prompt, 180)}
                    </div>
                </div>
            ) : null}
            <div className="flex flex-wrap gap-1">
                <span
                    className="rounded bg-surface-container-low px-1.5 py-0.5 text-[10px] text-on-surface">people {summary.people}</span>
                <span
                    className="rounded bg-surface-container-low px-1.5 py-0.5 text-[10px] text-on-surface">messages {summary.messages}</span>
                <span
                    className="rounded bg-surface-container-low px-1.5 py-0.5 text-[10px] text-on-surface">threads {summary.threads}</span>
            </div>
            {(diff.addedMessages.length || diff.removedMessages.length || diff.addedPeople.length || diff.removedPeople.length || diff.addedThreads.length || diff.removedThreads.length || diff.renamed) ? (
                <div className="space-y-1">
                    <div
                        className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Changes
                    </div>
                    <div className="flex flex-wrap gap-1">
                        {diff.renamed ? <span
                            className="rounded bg-amber-50 px-1.5 py-0.5 text-[10px] text-amber-700">renamed</span> : null}
                        {diff.addedPeople.length ? <span
                            className="rounded bg-emerald-50 px-1.5 py-0.5 text-[10px] text-emerald-700">+{diff.addedPeople.length} people</span> : null}
                        {diff.removedPeople.length ? <span
                            className="rounded bg-rose-50 px-1.5 py-0.5 text-[10px] text-rose-700">-{diff.removedPeople.length} people</span> : null}
                        {diff.addedMessages.length ? <span
                            className="rounded bg-emerald-50 px-1.5 py-0.5 text-[10px] text-emerald-700">+{diff.addedMessages.length} messages</span> : null}
                        {diff.removedMessages.length ? <span
                            className="rounded bg-rose-50 px-1.5 py-0.5 text-[10px] text-rose-700">-{diff.removedMessages.length} messages</span> : null}
                        {diff.addedThreads.length ? <span
                            className="rounded bg-emerald-50 px-1.5 py-0.5 text-[10px] text-emerald-700">+{diff.addedThreads.length} threads</span> : null}
                        {diff.removedThreads.length ? <span
                            className="rounded bg-rose-50 px-1.5 py-0.5 text-[10px] text-rose-700">-{diff.removedThreads.length} threads</span> : null}
                    </div>
                </div>
            ) : null}
            <div className="text-[11px] text-on-surface leading-relaxed">{summary.name || "Unnamed bundle"}</div>
            {activity.length ? (
                <div className="space-y-1">
                    <button
                        type="button"
                        onClick={() => setShowActivity((v) => !v)}
                        className="inline-flex items-center gap-1 text-[10px] font-semibold text-primary hover:underline"
                    >
                        <ChevronDown className={`h-3 w-3 transition-transform ${showActivity ? "rotate-180" : ""}`}/>
                        {showActivity ? "Hide tool activity" : "Show tool activity"}
                    </button>
                    {showActivity ? (
                        <div className="space-y-1">
                            {activity.map((item, idx) => (
                                <div key={`${item.tool}-${idx}`}
                                     className="rounded bg-surface-container-low px-2 py-1 text-[10px] text-on-surface-variant leading-relaxed">
                                    <span
                                        className="font-semibold text-on-surface">{getToolLabel(item.tool, item.args)}</span>
                                    {item.detail ? `: ${item.detail}` : ""}
                                </div>
                            ))}
                        </div>
                    ) : null}
                </div>
            ) : null}
            <button
                type="button"
                onClick={() => setShowRaw((v) => !v)}
                className="inline-flex items-center gap-1 text-[10px] font-semibold text-primary hover:underline"
            >
                <ChevronDown className={`h-3 w-3 transition-transform ${showRaw ? "rotate-180" : ""}`}/>
                {showRaw ? "Hide raw snapshot" : "Show raw snapshot"}
            </button>
            {showRaw ? (
                <pre
                    className="text-[10px] leading-relaxed text-on-surface-variant bg-surface-container-low/60 p-2 rounded border border-outline-variant/35 overflow-x-auto whitespace-pre-wrap break-words">
          {safePreviewJson(revision.entities_json, 2000)}
        </pre>
            ) : null}
        </div>
    );
}

function summarizeRevision(revision: DraftRevision) {
    let prompt = "";
    try {
        const transcript = JSON.parse(revision.transcript_json);
        if (Array.isArray(transcript)) {
            for (let i = transcript.length - 1; i >= 0; i--) {
                const item = transcript[i];
                if (item && item.role === "user" && typeof item.text === "string" && item.text.trim()) {
                    prompt = safePreviewText(item.text.trim(), 180);
                    break;
                }
            }
        }
    } catch {
    }

    let name = "";
    let people = 0;
    let messages = 0;
    let threads = 0;
    try {
        const entities = JSON.parse(revision.entities_json);
        name = typeof entities?.name === "string" ? entities.name : "";
        people = Array.isArray(entities?.people) ? entities.people.length : 0;
        messages = Array.isArray(entities?.messages) ? entities.messages.length : 0;
        threads = Array.isArray(entities?.threads) ? entities.threads.length : 0;
    } catch {
    }

    return {prompt, name, people, messages, threads};
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

function loopsForRevision(loops: CollectorLoop[], revisions: DraftRevision[], index: number) {
    const current = Date.parse(revisions[index].created_at);
    const previous = index > 0 ? Date.parse(revisions[index - 1].created_at) : Number.NEGATIVE_INFINITY;
    return loops.filter((loop) => {
        const at = Date.parse(loop.created_at);
        return at > previous && at <= current;
    });
}

function summarizeCollectorLoops(loops: CollectorLoop[]) {
    const items: Array<{ tool: string; detail: string; args?: unknown }> = [];
    for (const loop of loops) {
        try {
            const toolCalls = JSON.parse(loop.tool_calls_json || "[]");
            for (const call of Array.isArray(toolCalls) ? toolCalls : []) {
                const tool = typeof call?.name === "string" ? call.name : "tool";
                const args = call?.args ?? {};
                let detail = "";
                if (tool === "fts_search" || tool === "vector_search" || tool === "find_people") {
                    detail = safePreviewText(typeof args?.query === "string" ? args.query : JSON.stringify(args), 120);
                } else if (tool === "get_message" && args?.message_id) {
                    detail = `message ${args.message_id}`;
                } else if (tool === "get_thread" && args?.thread_id) {
                    detail = `thread ${args.thread_id}`;
                } else if (tool === "propose_backfill" && Array.isArray(args?.candidate_message_ids)) {
                    detail = `${args.candidate_message_ids.length} candidates`;
                } else if (tool === "propose_bundle") {
                    detail = "bundle staged";
                } else {
                    detail = safePreviewJson(args, 120);
                }
                items.push({tool, detail, args});
            }
        } catch {
        }
    }
    return items;
}
