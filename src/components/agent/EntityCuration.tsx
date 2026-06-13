"use client";
import {
    type Dispatch,
    type ReactNode,
    type SetStateAction,
    useCallback,
    useEffect,
    useMemo,
    useRef,
    useState
} from "react";
import {
    Archive,
    Check,
    Clock,
    FileText,
    LoaderCircle,
    MessageSquare,
    RefreshCw,
    Save,
    Terminal,
    Users,
    Wrench
} from "lucide-react";
import {safePreviewText} from "./panel-safety";
import {getToolLabel} from "@/lib/tool-labels";
import {normalizeLogsResponse} from "@/lib/agent-logs";

export interface EntityBundle {
    name: string;
    summary_hint?: string;
    people?: Array<{ person_id: number; display_name: string; role?: string; evidence_message_ids?: number[] }>;
    messages?: Array<{
        message_id: number;
        subject?: string;
        date?: string;
        include_reason?: string;
        agent_confidence?: number
    }>;
    threads?: Array<{ thread_id: number; subject?: string; message_count?: number; include_reason?: string }>;
}

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

interface EntityCurationProps {
    bundle: EntityBundle | null;
    onChange: (next: EntityBundle) => void;       // called when checkboxes toggle / name edits
    onCommit: () => void;
    committing: boolean;
    entityLabel?: string;
    saveLabel?: string;
    emptyTitle?: string;
    emptyDescription?: string;
    sessionType?: "collector" | "project_compile" | "concept_compile" | "person_enrich" | "dashboard";
    entityId?: string;
}

export default function EntityCuration({
                                           bundle,
                                           onChange,
                                           onCommit,
                                           committing,
                                           entityLabel = "Project",
                                           saveLabel = "Save project",
                                           emptyTitle = "Bundle preview",
                                           emptyDescription = "Proposed people, messages, and threads will appear here for review.",
                                           sessionType,
                                           entityId,
                                       }: EntityCurationProps) {
    // Track which entity ids are "kept" — defaults to everything present in the bundle.
    const [keepPeople, setKeepPeople] = useState<Set<number>>(new Set());
    const [keepMessages, setKeepMessages] = useState<Set<number>>(new Set());
    const [keepThreads, setKeepThreads] = useState<Set<number>>(new Set());
    const [name, setName] = useState(bundle?.name ?? "");

    // Agent Operations Log Sidebar state
    const [showLogs, setShowLogs] = useState(false);
    const [logs, setLogs] = useState<AgentLoopLog[]>([]);
    const [loadingLogs, setLoadingLogs] = useState(false);
    const [logsError, setLogsError] = useState<string | null>(null);

    // When a server-driven bundle (re)load resets the keep-sets below, the
    // resulting `filtered` change must NOT be echoed back to the server: on the
    // render before the keep-sets catch up, `filtered` is the bundle filtered by
    // the stale (often empty) sets, and autosaving that would clobber the freshly
    // staged bundle. This flag suppresses exactly that one autosave.
    const skipAutosave = useRef(false);

    // Reset selections whenever a new bundle arrives from the agent.
    useEffect(() => {
        if (!bundle) return;
        skipAutosave.current = true;
        setKeepPeople(new Set(bundle.people?.map((p) => p.person_id) ?? []));
        setKeepMessages(new Set(bundle.messages?.map((m) => m.message_id) ?? []));
        setKeepThreads(new Set(bundle.threads?.map((t) => t.thread_id) ?? []));
        setName(bundle.name ?? "");
    }, [bundle]);

    const filtered = useMemo<EntityBundle | null>(() => {
        if (!bundle) return null;
        return {
            ...bundle,
            name,
            people: (bundle.people ?? []).filter((p) => keepPeople.has(p.person_id)),
            messages: (bundle.messages ?? []).filter((m) => keepMessages.has(m.message_id)),
            threads: (bundle.threads ?? []).filter((t) => keepThreads.has(t.thread_id)),
        };
    }, [bundle, keepPeople, keepMessages, keepThreads, name]);

    // Push user edits up. Skip the change that a fresh bundle load triggers so we
    // never autosave a stale/empty filtered bundle over the staged one.
    useEffect(() => {
        if (!filtered) return;
        if (skipAutosave.current) {
            skipAutosave.current = false;
            return;
        }
        onChange(filtered);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [filtered]);

    const fetchLogs = useCallback(async () => {
        if (!sessionType || !entityId) return;
        setLoadingLogs(true);
        setLogsError(null);
        try {
            const res = await fetch(`/api/agents/logs?type=${sessionType}&entityId=${entityId}`);
            if (!res.ok) throw new Error("Unable to load trace.");
            setLogs(normalizeLogsResponse<AgentLoopLog>(await res.json()).loops);
        } catch (err) {
            setLogsError(err instanceof Error ? err.message : String(err));
        } finally {
            setLoadingLogs(false);
        }
    }, [sessionType, entityId]);

    useEffect(() => {
        if (showLogs) {
            fetchLogs();
        }
    }, [showLogs, fetchLogs]);

    // Grouping messages chronologically (sorted descending by date)
    const sortedMessages = useMemo(() => {
        if (!bundle?.messages) return [];
        return [...bundle.messages].sort((a, b) => {
            if (!a.date) return 1;
            if (!b.date) return -1;
            return new Date(b.date).getTime() - new Date(a.date).getTime();
        });
    }, [bundle?.messages]);

    const groupedMessages = useMemo(() => {
        const groups: { [key: string]: typeof sortedMessages } = {};
        for (const m of sortedMessages) {
            let key = "Undated";
            if (m.date) {
                try {
                    const d = new Date(m.date);
                    if (!isNaN(d.getTime())) {
                        key = d.toLocaleString("default", {month: "long", year: "numeric"});
                    }
                } catch {
                    // ignore
                }
            }
            if (!groups[key]) groups[key] = [];
            groups[key].push(m);
        }
        return groups;
    }, [sortedMessages]);

    const groupedKeys = useMemo(() => {
        const keys = Object.keys(groupedMessages);
        const undated = keys.filter((k) => k === "Undated");
        const dated = keys.filter((k) => k !== "Undated");
        dated.sort((a, b) => new Date(b).getTime() - new Date(a).getTime());
        return [...dated, ...undated];
    }, [groupedMessages]);

    if (!bundle) {
        return (
            <section
                className="flex h-full min-h-0 items-center justify-center rounded-lg border border-dashed border-outline-variant/70 bg-surface-container-low p-8 text-center shadow-sm">
                <div className="max-w-sm space-y-3">
                    <div
                        className="mx-auto flex h-10 w-10 items-center justify-center rounded bg-background text-on-surface-variant">
                        <Archive className="h-5 w-5"/>
                    </div>
                    <div className="text-sm font-semibold text-on-surface">{emptyTitle}</div>
                    <p className="text-sm leading-6 text-on-surface-variant">
                        {emptyDescription}
                    </p>
                </div>
            </section>
        );
    }

    const toggle = <T, >(setter: Dispatch<SetStateAction<Set<T>>>, id: T) =>
        setter((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });

    const totalSelected =
        keepPeople.size + keepMessages.size + keepThreads.size;
    const totalAvailable =
        (bundle.people?.length ?? 0) + (bundle.messages?.length ?? 0) + (bundle.threads?.length ?? 0);

    return (
        <section
            className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-outline-variant/50 bg-surface-container-lowest shadow-sm">
            <header className="border-b border-outline-variant/40 bg-surface-container-low px-4 py-4 shrink-0">
                <div className="mb-3 flex items-center justify-between gap-3">
                    <div className="min-w-0">
                        <div className="text-label-caps font-label-caps text-on-surface-variant">Draft bundle</div>
                        <div className="mt-0.5 text-sm font-semibold text-on-surface">Review before saving</div>
                    </div>
                    <div className="flex items-center gap-2">
                        {sessionType && entityId && (
                            <button
                                type="button"
                                onClick={() => setShowLogs(!showLogs)}
                                className={`inline-flex items-center gap-1.5 rounded border px-2.5 py-1 text-[11px] font-semibold transition ${
                                    showLogs
                                        ? "border-primary bg-primary text-white"
                                        : "border-outline-variant/60 bg-background text-on-surface-variant hover:bg-surface-container"
                                }`}
                            >
                                <Terminal className="h-3 w-3"/>
                                {showLogs ? "Hide Trace" : "Show Trace"}
                            </button>
                        )}
                        <span
                            className="shrink-0 rounded border border-outline-variant/60 bg-background px-2.5 py-1 font-mono text-[11px] text-on-surface-variant">
              {totalSelected}/{totalAvailable} kept
            </span>
                    </div>
                </div>
                <label className="text-label-caps font-label-caps text-on-surface-variant">{entityLabel} name</label>
                <input
                    type="text"
                    className="mt-1.5 w-full rounded border border-outline-variant/70 bg-background px-3 py-2.5 text-sm font-semibold text-on-surface shadow-inner focus:outline-none focus:ring-2 focus:ring-primary/30"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                />
                {bundle.summary_hint && (
                    <p className="mt-2 text-sm leading-6 text-on-surface-variant">{bundle.summary_hint}</p>
                )}
            </header>

            <div className="flex-1 min-h-0 flex overflow-hidden">
                {/* Main Curation Checklist */}
                <div className="flex-1 overflow-y-auto p-4 space-y-6">
                    {bundle.people && bundle.people.length > 0 && (
                        <section className="space-y-2">
                            <SectionHeader icon={<Users className="h-4 w-4"/>} title="People" count={keepPeople.size}
                                           total={bundle.people.length}/>
                            <ul className="grid grid-cols-1 md:grid-cols-2 gap-2">
                                {bundle.people.map((p) => (
                                    <CurationRow
                                        key={p.person_id}
                                        checked={keepPeople.has(p.person_id)}
                                        onToggle={() => toggle(setKeepPeople, p.person_id)}
                                    >
                                        <div className="min-w-0 flex-1">
                                            <div
                                                className="truncate text-sm font-semibold text-on-surface">{p.display_name}</div>
                                            {p.role && (
                                                <div
                                                    className="mt-0.5 truncate text-xs text-on-surface-variant font-medium">{p.role}</div>
                                            )}
                                            {p.evidence_message_ids && p.evidence_message_ids.length > 0 && (
                                                <div
                                                    className="mt-1.5 flex flex-wrap gap-1 items-center text-[10px] text-on-surface-variant/90">
                                                    <span
                                                        className="font-semibold text-on-surface-variant">Evidence:</span>
                                                    {p.evidence_message_ids.map((id) => (
                                                        <span key={id}
                                                              className="rounded bg-surface-container-low px-1 py-0.5 font-mono text-[9px] border border-outline-variant/30">
                              msg:{id}
                            </span>
                                                    ))}
                                                </div>
                                            )}
                                        </div>
                                    </CurationRow>
                                ))}
                            </ul>
                        </section>
                    )}

                    {bundle.messages && bundle.messages.length > 0 && (
                        <section className="space-y-4">
                            <SectionHeader icon={<FileText className="h-4 w-4"/>} title="Message Timeline"
                                           count={keepMessages.size} total={bundle.messages.length}/>

                            <div className="relative border-l border-outline-variant/40 ml-3.5 pl-6 space-y-6">
                                {groupedKeys.map((groupKey) => (
                                    <div key={groupKey} className="relative space-y-3">
                                        {/* Timeline Node Badge */}
                                        <div
                                            className="absolute -left-[31px] top-1 flex h-4.5 w-4.5 items-center justify-center rounded-full border border-primary/40 bg-background shadow-sm">
                                            <div className="h-1.5 w-1.5 rounded-full bg-primary"/>
                                        </div>

                                        <h4 className="text-xs font-bold text-primary tracking-wider uppercase bg-surface-container-lowest/80 backdrop-blur-sm sticky top-0 py-0.5 z-10">
                                            {groupKey}
                                        </h4>

                                        <ul className="space-y-2">
                                            {groupedMessages[groupKey].map((m) => (
                                                <CurationRow
                                                    key={m.message_id}
                                                    checked={keepMessages.has(m.message_id)}
                                                    onToggle={() => toggle(setKeepMessages, m.message_id)}
                                                >
                                                    <div className="min-w-0 flex-1">
                                                        <div
                                                            className="truncate text-sm font-semibold text-on-surface">{m.subject || `Message #${m.message_id}`}</div>
                                                        <div className="mt-1.5 space-y-1.5">
                                                            {m.include_reason && (
                                                                <div
                                                                    className="text-xs text-on-surface-variant/90 leading-relaxed bg-surface-container-low/50 p-2 rounded border border-outline-variant/30 whitespace-normal">
                                                                    {m.include_reason}
                                                                </div>
                                                            )}
                                                            <div
                                                                className="flex flex-wrap items-center justify-between gap-2 text-[10px] text-on-surface-variant">
                                                                {m.date && <span
                                                                    className="font-mono">{m.date.slice(0, 10)}</span>}
                                                                {m.agent_confidence !== undefined && (
                                                                    <span className={`rounded px-1.5 py-0.5 font-bold ${
                                                                        m.agent_confidence >= 0.85
                                                                            ? "bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-400"
                                                                            : "bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-400"
                                                                    }`}>
                                    {Math.round(m.agent_confidence * 100)}% confidence
                                  </span>
                                                                )}
                                                            </div>
                                                        </div>
                                                    </div>
                                                </CurationRow>
                                            ))}
                                        </ul>
                                    </div>
                                ))}
                            </div>
                        </section>
                    )}

                    {bundle.threads && bundle.threads.length > 0 && (
                        <section className="space-y-2">
                            <SectionHeader icon={<MessageSquare className="h-4 w-4"/>} title="Threads"
                                           count={keepThreads.size} total={bundle.threads.length}/>
                            <ul className="space-y-2">
                                {bundle.threads.map((t) => (
                                    <CurationRow
                                        key={t.thread_id}
                                        checked={keepThreads.has(t.thread_id)}
                                        onToggle={() => toggle(setKeepThreads, t.thread_id)}
                                    >
                                        <div className="min-w-0 flex-1">
                                            <div
                                                className="truncate text-sm font-semibold text-on-surface">{t.subject || `Thread #${t.thread_id}`}</div>
                                            <div className="mt-1.5 space-y-1.5">
                                                {t.include_reason && (
                                                    <div
                                                        className="text-xs text-on-surface-variant/90 leading-relaxed bg-surface-container-low/50 p-2 rounded border border-outline-variant/30 whitespace-normal">
                                                        {t.include_reason}
                                                    </div>
                                                )}
                                                <div
                                                    className="flex items-center justify-between gap-2 text-[10px] text-on-surface-variant">
                                                    {t.message_count ? <span
                                                        className="font-mono">{t.message_count} messages</span> : null}
                                                </div>
                                            </div>
                                        </div>
                                    </CurationRow>
                                ))}
                            </ul>
                        </section>
                    )}
                </div>

                {/* Collapsible Agent Reasoning Sidebar */}
                {showLogs && (
                    <div
                        className="w-96 shrink-0 border-l border-outline-variant/45 bg-surface-container-low/60 flex flex-col min-h-0 overflow-hidden">
                        <header
                            className="flex items-center justify-between border-b border-outline-variant/35 bg-surface-container-low px-4 py-3 shrink-0">
                            <div className="text-xs font-semibold text-on-surface flex items-center gap-1.5">
                                <Terminal className="h-3.5 w-3.5 text-primary"/>
                                Agent Execution Trace
                            </div>
                            <button
                                onClick={fetchLogs}
                                disabled={loadingLogs}
                                className="p-1 rounded hover:bg-outline-variant/30 text-on-surface-variant disabled:opacity-50"
                                title="Refresh trace"
                            >
                                <RefreshCw className={`h-3.5 w-3.5 ${loadingLogs ? "animate-spin" : ""}`}/>
                            </button>
                        </header>
                        <div className="flex-1 overflow-y-auto p-4 space-y-4">
                            {loadingLogs && logs.length === 0 ? (
                                <div
                                    className="text-xs text-on-surface-variant flex items-center justify-center py-10 gap-2">
                                    <LoaderCircle className="h-4 w-4 animate-spin text-primary"/>
                                    Loading trace...
                                </div>
                            ) : logsError ? (
                                <div className="text-xs text-red-600 bg-red-50 border border-red-200 rounded p-3">
                                    {logsError}
                                </div>
                            ) : logs.length === 0 ? (
                                <div className="text-xs text-on-surface-variant text-center py-10">
                                    No trace available for this run.
                                </div>
                            ) : (
                                logs.map((log) => <LogStepCard key={log.step_index} log={log}/>)
                            )}
                        </div>
                    </div>
                )}
            </div>

            <footer
                className="flex items-center justify-between gap-3 border-t border-outline-variant/40 bg-surface-container-low px-4 py-3 shrink-0">
                <div className="min-w-0 text-xs text-on-surface-variant">
                    <span className="font-semibold text-on-surface">{totalSelected}</span> selected for the draft
                </div>
                <button
                    className="inline-flex h-10 shrink-0 items-center gap-2 rounded bg-primary px-4 text-sm font-semibold text-white shadow-sm transition hover:opacity-90 disabled:opacity-50"
                    onClick={onCommit}
                    disabled={committing || totalSelected === 0 || !name.trim()}
                >
                    {committing ? <Archive className="h-4 w-4 animate-pulse"/> : <Save className="h-4 w-4"/>}
                    <span>{committing ? "Saving" : saveLabel}</span>
                </button>
            </footer>
        </section>
    );
}

function LogStepCard({log}: { log: AgentLoopLog }) {
    const [showReasoning, setShowReasoning] = useState(false);
    const durationSec = log.duration_ms ? (log.duration_ms / 1000).toFixed(1) + "s" : "";
    const toolCalls = useMemo(() => {
        try {
            // A step with no tool calls is serialized as the literal string "null"
            // (Go marshals a nil slice that way), so guard against non-arrays.
            const parsed = JSON.parse(log.tool_calls_json || "[]");
            return Array.isArray(parsed) ? parsed : [];
        } catch {
            return [];
        }
    }, [log.tool_calls_json]);

    return (
        <div
            className="rounded border border-outline-variant/40 bg-surface-container-lowest p-3 space-y-2.5 text-xs shadow-sm">
            <div className="flex items-center justify-between">
        <span className="font-semibold text-on-surface bg-primary/10 text-primary px-1.5 py-0.5 rounded text-[10px]">
          Step {log.step_index}
        </span>
                {durationSec && (
                    <span className="text-[10px] text-on-surface-variant flex items-center gap-1 font-mono">
            <Clock className="h-3 w-3"/>
                        {durationSec}
          </span>
                )}
            </div>

            {toolCalls.length > 0 && (
                <div className="space-y-1">
                    <div
                        className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider flex items-center gap-1">
                        <Wrench className="h-3 w-3"/>
                        Tools
                    </div>
                    <div className="flex flex-wrap gap-1">
                        {toolCalls.map((tc: any, i: number) => (
                            <span key={i}
                                  className="rounded bg-surface-container-low px-1.5 py-0.5 font-mono text-[9px] border border-outline-variant/20 text-on-surface"
                                  title={safePreviewText(JSON.stringify(tc.args ?? {}), 180)}>
                {getToolLabel(tc.name, tc.args)}
              </span>
                        ))}
                    </div>
                </div>
            )}

            {log.assistant_text && (
                <div
                    className="text-on-surface whitespace-pre-wrap leading-relaxed border-l-2 border-primary/20 pl-2 italic">
                    {log.assistant_text}
                </div>
            )}

            {log.reasoning_text && (
                <div className="space-y-1">
                    <button
                        onClick={() => setShowReasoning(!showReasoning)}
                        className="text-[10px] font-semibold text-primary hover:underline flex items-center gap-1"
                    >
                        {showReasoning ? "Hide reasoning" : "Show reasoning"}
                    </button>
                    {showReasoning && (
                        <div
                            className="text-[11px] leading-relaxed text-on-surface-variant/90 bg-surface-container-low/40 p-2 rounded border border-outline-variant/35 font-mono whitespace-pre-wrap overflow-x-auto max-h-48">
                            {log.reasoning_text}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

function CurationRow({
                         checked,
                         onToggle,
                         children,
                     }: {
    checked: boolean;
    onToggle: () => void;
    children: ReactNode;
}) {
    return (
        <li
            className={`group flex items-start gap-3 rounded-lg border px-3 py-2.5 transition ${
                checked
                    ? "border-outline-variant/45 bg-background"
                    : "border-outline-variant/30 bg-surface-container-low opacity-70"
            }`}
        >
            <button
                type="button"
                aria-pressed={checked}
                aria-label={checked ? "Remove from draft" : "Keep in draft"}
                onClick={onToggle}
                className={`mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded border transition ${
                    checked
                        ? "border-primary bg-primary text-white"
                        : "border-outline-variant bg-surface-container-lowest text-transparent group-hover:border-outline"
                }`}
            >
                <Check className="h-3.5 w-3.5"/>
            </button>
            {children}
        </li>
    );
}

function SectionHeader({
                           icon,
                           title,
                           count,
                           total,
                       }: {
    icon: ReactNode;
    title: string;
    count: number;
    total: number;
}) {
    return (
        <div className="flex items-center justify-between gap-3">
            <h3 className="flex items-center gap-2 text-label-caps font-label-caps text-on-surface-variant">
                {icon}
                {title}
            </h3>
            <span
                className="rounded border border-outline-variant/50 bg-surface-container-low px-2 py-0.5 font-mono text-[10px] text-on-surface-variant">
        {count}/{total}
      </span>
        </div>
    );
}
