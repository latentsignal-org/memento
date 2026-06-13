"use client";
import {useCallback, useEffect, useMemo, useState} from "react";
import Link from "next/link";
import {
    Activity,
    AlertTriangle,
    ArrowLeft,
    ArrowRightLeft,
    BarChart2,
    CheckCircle,
    ChevronDown,
    ChevronRight,
    Clock,
    Cpu,
    Database,
    Eye,
    Filter,
    Mail,
    Search,
    Terminal,
} from "lucide-react";
import {Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger,} from "@/components/ui/sheet";
import {normalizeLogsResponse} from "@/lib/agent-logs";

interface AgentSession {
    id: number;
    session_type: string;
    entity_id: string;
    status: string;
    provider?: string;
    model?: string;
    user_message?: string;
    total_estimated_input_tokens: number;
    total_estimated_output_tokens: number;
    total_estimated_tool_result_tokens: number;
    total_model_input_tokens: number;
    total_model_output_tokens: number;
    total_model_tokens: number;
    created_at: string;
    updated_at: string;
}

interface DebugSystemInfo {
    working_directory: string;
    msgvault_db_path: string;
    provider: string;
    model: string;
    model_base_url: string;
}

// sessionTitle picks the most useful label for a run in the sidebar and
// header. For collector and dashboard runs the entity_id is opaque
// (a draft id or the literal "dashboard"), so the user-typed message
// is more recognizable. For project/concept/person agents the auto-
// generated user_message is boilerplate ("Compile the narrative...")
// so we keep the entity_id slug instead.
function sessionTitle(s: AgentSession): string {
    const msg = (s.user_message ?? "").trim();
    if (msg && (s.session_type === "collector" || s.session_type === "dashboard")) {
        return msg.length > 60 ? msg.slice(0, 57) + "…" : msg;
    }
    return s.entity_id;
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
    estimated_input_tokens: number;
    estimated_output_tokens: number;
    estimated_tool_result_tokens: number;
    model_input_tokens: number;
    model_output_tokens: number;
    model_total_tokens: number;
    usage_json: string;
    created_at: string;
}

interface AgentToolCallTrace {
    id: number;
    session_id: number;
    step_index: number;
    call_index: number;
    call_id: string;
    tool_name: string;
    tool_kind: string;
    lock_key: string;
    args_json: string;
    result_json: string;
    error_message: string;
    queued_at: string;
    started_at: string;
    finished_at: string;
    duration_ms: number;
    queue_wait_ms: number;
    lock_wait_ms: number;
    parallel_limit: number;
    batch_size: number;
    args_bytes: number;
    result_bytes: number;
    estimated_result_tokens: number;
    created_at: string;
}

interface AskSessionDebugLink {
    id: number;
    slug: string;
    title: string;
    turn_id: number;
    turn_index: number;
}

function extractToolName(toolCallsJSON: string): string | null {
    if (!toolCallsJSON || toolCallsJSON === "[]" || toolCallsJSON === "null") return null;
    try {
        const parsed = JSON.parse(toolCallsJSON);
        const calls = Array.isArray(parsed) ? parsed : [parsed];
        return (
            calls[0]?.name ??
            calls[0]?.function?.name ??
            calls[0]?.functionCall?.name ??
            null
        );
    } catch {
        return null;
    }
}

function actualMaxConcurrency(traces: AgentToolCallTrace[]): number {
    const points: Array<{ t: number; delta: number }> = [];
    for (const trace of traces) {
        const start = Date.parse(trace.started_at);
        const finish = Date.parse(trace.finished_at);
        if (!Number.isFinite(start) || !Number.isFinite(finish) || finish < start) continue;
        points.push({t: start, delta: 1}, {t: finish, delta: -1});
    }
    points.sort((a, b) => a.t - b.t || b.delta - a.delta);
    let current = 0;
    let max = 0;
    for (const point of points) {
        current += point.delta;
        max = Math.max(max, current);
    }
    return max;
}

interface ToolStat {
    tool: string;
    count: number;
    totalMs: number;
    totalPayloadChars: number;
}

function buildToolStats(logs: AgentLoopLog[]): ToolStat[] {
    const map = new Map<string, ToolStat>();
    for (const l of logs) {
        let name = extractToolName(l.tool_calls_json);
        if (name === null && l.tool_calls_json && l.tool_calls_json !== "null" && l.tool_calls_json !== "[]") {
            name = "(incomplete)";
        }
        if (!name) continue;
        const existing = map.get(name);
        const payloadChars = l.input_content.length + l.tool_calls_json.length;
        if (existing) {
            existing.count++;
            existing.totalMs += l.duration_ms;
            existing.totalPayloadChars += payloadChars;
        } else {
            map.set(name, {tool: name, count: 1, totalMs: l.duration_ms, totalPayloadChars: payloadChars});
        }
    }
    return Array.from(map.values()).sort((a, b) => b.totalMs - a.totalMs);
}

interface SizeInfo {
    bytes: number;
    estimatedTokens: number;
}

function computeSize(str: string): SizeInfo {
    const bytes = new TextEncoder().encode(str).length;
    const estimatedTokens = Math.round(str.length / 4);
    return {bytes, estimatedTokens};
}

function formatBytes(bytes: number): string {
    if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${bytes} B`;
}

function formatDuration(ms: number) {
    if (ms >= 60000) {
        const m = Math.floor(ms / 60000);
        const s = ((ms % 60000) / 1000).toFixed(0);
        return `${m}m ${s}s`;
    }
    if (ms >= 1000) return `${Math.round(ms / 1000)}s`;
    return `${ms}ms`;
}

function formatNumber(n: number): string {
    return n.toLocaleString();
}

type DurationVariant = "red" | "amber" | "blue";

function getDurationVariant(ms: number): DurationVariant {
    if (ms > 60000) return "red";
    if (ms > 10000) return "amber";
    return "blue";
}

const DURATION_CLASSES: Record<DurationVariant, {
    text: string; bg: string; badge: string; border: string;
}> = {
    red: {
        text: "text-red-600",
        bg: "bg-red-500",
        badge: "text-red-700 bg-red-50 border-red-200",
        border: "border-red-200",
    },
    amber: {
        text: "text-amber-600",
        bg: "bg-amber-500",
        badge: "text-amber-700 bg-amber-50 border-amber-200",
        border: "border-amber-200",
    },
    blue: {
        text: "text-blue-600",
        bg: "bg-blue-400",
        badge: "text-blue-700 bg-blue-50 border-blue-200",
        border: "border-outline-variant",
    },
};

function toolTextColor(tool: string): string {
    if (tool.includes("search")) return "text-cyan-700";
    if (tool.startsWith("get_")) return "text-sky-700";
    if (tool.startsWith("find_")) return "text-violet-700";
    if (tool.startsWith("propose_")) return "text-orange-700";
    if (tool.startsWith("detect_")) return "text-rose-700";
    if (tool.startsWith("write_") || tool.startsWith("generate")) return "text-emerald-700";
    return "text-amber-700";
}

// Session type uses app-adjacent muted tints that read well on warm white
const SESSION_TYPE_CLASSES: Record<string, string> = {
    collector: "bg-secondary text-secondary-foreground border-outline-variant",
    project_compile: "bg-primary-fixed text-on-primary-fixed-variant border-outline-variant",
    concept_compile: "bg-secondary-container text-on-secondary-container border-outline-variant",
    person_enrich: "bg-tertiary-fixed text-on-tertiary-fixed border-outline-variant",
};

function sessionTypeClass(type: string) {
    return SESSION_TYPE_CLASSES[type] ?? "bg-muted text-muted-foreground border-outline-variant";
}

function tryFormatJSON(jsonStr: string) {
    if (!jsonStr) return "";
    try {
        return JSON.stringify(JSON.parse(jsonStr), null, 2);
    } catch {
        return jsonStr;
    }
}

// ── Text viewer helpers ───────────────────────────────────────────────────────

interface MessageRecord {
    message_id?: number;
    date?: string;
    subject?: string;
    sender_canonical_name?: string;
    sender_primary_email?: string;
    snippet?: string;
    body_text?: string;
    direction?: string;
}

function stripHtml(html: string): string {
    return html
        .replace(/<style[^>]*>[\s\S]*?<\/style>/gi, "")
        .replace(/<script[^>]*>[\s\S]*?<\/script>/gi, "")
        .replace(/<[^>]+>/g, "")
        .replace(/&nbsp;/g, " ")
        .replace(/&lt;/g, "<")
        .replace(/&gt;/g, ">")
        .replace(/&amp;/g, "&")
        .replace(/&quot;/g, '"')
        .replace(/\n{3,}/g, "\n\n")
        .trim();
}

function looksLikeMessages(arr: unknown[]): arr is MessageRecord[] {
    return arr.length > 0 && typeof (arr[0] as Record<string, unknown>)?.message_id !== "undefined";
}

// Handles two real storage formats:
// 1. tool_results_json  → [{name, call_id, result: [...messages] | singleMessage}]
// 2. input_content      → [{type:"function_result", result:[{type:"text", text:"[...]"}]}]
function extractMessageArrays(content: string): MessageRecord[][] | null {
    try {
        const parsed = JSON.parse(content);
        const topLevel: unknown[] = Array.isArray(parsed) ? parsed : [parsed];
        const arrays: MessageRecord[][] = [];

        for (const item of topLevel) {
            if (!item || typeof item !== "object") continue;
            const entry = item as Record<string, unknown>;

            // Format 1: tool_results_json — result is already the parsed tool output
            if (entry.result !== undefined && entry.name !== undefined) {
                const raw = entry.result;
                const asArr = Array.isArray(raw) ? raw : [raw];
                if (looksLikeMessages(asArr)) {
                    arrays.push(asArr as MessageRecord[]);
                    continue;
                }
            }

            // Format 2: input_content — result is [{type:"text", text: JSON string}]
            if (entry.type === "function_result" && Array.isArray(entry.result)) {
                for (const r of entry.result as Record<string, unknown>[]) {
                    if (r?.type === "text" && typeof r.text === "string") {
                        try {
                            const inner = JSON.parse(r.text as string);
                            const asArr = Array.isArray(inner) ? inner : [inner];
                            if (looksLikeMessages(asArr)) arrays.push(asArr as MessageRecord[]);
                        } catch { /* not a message JSON */
                        }
                    }
                }
            }
        }

        return arrays.length > 0 ? arrays : null;
    } catch {
        return null;
    }
}

function MessageCard({msg}: { msg: MessageRecord }) {
    const [expanded, setExpanded] = useState(false);
    const body = msg.body_text ? stripHtml(msg.body_text) : null;
    const dateStr = msg.date
        ? new Date(msg.date).toLocaleString([], {dateStyle: "medium", timeStyle: "short"})
        : null;

    return (
        <div className="rounded-lg border border-outline-variant bg-white overflow-hidden">
            <div className="px-4 py-3 space-y-1">
                <p className="text-sm font-semibold text-on-surface leading-snug">
                    {msg.subject ?? "(no subject)"}
                </p>
                <div className="flex items-center gap-1.5 text-xs text-on-surface-variant">
                    <Mail className="h-3 w-3 shrink-0"/>
                    <span className="font-medium">{msg.sender_canonical_name ?? msg.sender_primary_email}</span>
                    {msg.sender_primary_email && msg.sender_canonical_name && (
                        <span className="text-on-surface-variant/60">· {msg.sender_primary_email}</span>
                    )}
                    {dateStr && <span className="ml-auto shrink-0 text-on-surface-variant/60">{dateStr}</span>}
                </div>
                {msg.snippet && (
                    <p className="text-xs text-on-surface-variant leading-relaxed line-clamp-2 italic">
                        {msg.snippet}
                    </p>
                )}
            </div>
            {body && (
                <>
                    <div className="border-t border-outline-variant/50 px-4 py-2 flex items-center justify-between">
            <span className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">
              Body
            </span>
                        <button
                            onClick={() => setExpanded((e) => !e)}
                            className="text-[10px] text-primary hover:underline"
                        >
                            {expanded ? "Collapse" : "Expand"}
                        </button>
                    </div>
                    {expanded && (
                        <div className="px-4 pb-4">
              <pre
                  className="text-[11px] text-on-surface whitespace-pre-wrap leading-relaxed font-sans max-h-72 overflow-y-auto bg-surface-container-low rounded p-3 border border-outline-variant">
                {body}
              </pre>
                        </div>
                    )}
                </>
            )}
        </div>
    );
}

// ── PreBox: scrollable code block with per-instance wrap toggle ───────────────

function PreBox({
                    label,
                    labelColor = "text-on-surface-variant",
                    labelIcon,
                    content,
                    rawContent,
                    bgClass = "bg-surface-container-low",
                    borderClass = "border-outline-variant",
                    textClass = "text-on-surface",
                }: {
    label: string;
    labelColor?: string;
    labelIcon?: React.ReactNode;
    content: string;
    rawContent?: string;
    bgClass?: string;
    borderClass?: string;
    textClass?: string;
}) {
    const [wrapped, setWrapped] = useState(false);
    const messageArrays = useMemo(() => extractMessageArrays(content), [content]);
    const size = useMemo(() => computeSize(rawContent ?? content), [rawContent, content]);

    return (
        <div className="min-w-0">
            <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 mb-1.5">
                <div
                    className={`flex min-w-0 items-center gap-1.5 text-xs font-bold uppercase tracking-wider font-mono ${labelColor}`}>
                    {labelIcon}
                    {label}
                    <span
                        className="ml-1 truncate text-[11px] font-normal normal-case text-on-surface-variant font-sans">
            {formatBytes(size.bytes)} · ~{formatNumber(size.estimatedTokens)} tok
          </span>
                </div>
                <div className="flex items-center gap-1.5">
                    {messageArrays && (
                        <Sheet>
                            <SheetTrigger
                                className="text-xs font-semibold px-1.5 py-0.5 rounded border bg-primary-fixed text-on-primary-fixed-variant border-outline-variant hover:bg-primary-fixed/70 transition flex items-center gap-1"
                                title="View parsed messages"
                            >
                                <Eye className="h-2.5 w-2.5"/>
                                View
                            </SheetTrigger>
                            <SheetContent side="right" className="w-[560px] overflow-y-auto flex flex-col gap-0 p-0">
                                <SheetHeader className="px-5 py-4 border-b border-outline-variant shrink-0">
                                    <SheetTitle className="text-sm font-bold text-on-surface">
                                        Message Results
                                        <span className="ml-2 text-xs font-normal text-on-surface-variant">
                      {messageArrays.reduce((n, a) => n + a.length, 0)} messages
                    </span>
                                    </SheetTitle>
                                </SheetHeader>
                                <div className="flex-1 overflow-y-auto px-5 py-4 space-y-3 bg-background">
                                    {messageArrays.flatMap((arr, i) =>
                                        arr.map((msg) => <MessageCard key={`${i}-${msg.message_id}`} msg={msg}/>)
                                    )}
                                </div>
                            </SheetContent>
                        </Sheet>
                    )}
                    <button
                        onClick={() => setWrapped((w) => !w)}
                        className={`text-xs font-semibold px-1.5 py-0.5 rounded border transition ${
                            wrapped
                                ? "bg-primary-fixed text-on-primary-fixed-variant border-outline-variant"
                                : "bg-surface-container text-on-surface-variant border-outline-variant hover:bg-surface-container-high"
                        }`}
                        title={wrapped ? "Disable text wrap" : "Wrap long lines"}
                    >
                        {wrapped ? "Unwrap" : "Wrap"}
                    </button>
                </div>
            </div>
            <pre
                className={`max-w-full p-3 rounded-lg border font-mono text-[10px] max-h-56 leading-relaxed ${bgClass} ${borderClass} ${textClass} ${
                    wrapped ? "whitespace-pre-wrap break-all overflow-y-auto" : "overflow-auto whitespace-pre"
                }`}
            >
        {content}
      </pre>
        </div>
    );
}

// ── Waterfall chart ──────────────────────────────────────────────────────────

function WaterfallChart({logs, onStepClick}: { logs: AgentLoopLog[]; onStepClick?: (stepIndex: number) => void }) {
    const maxMs = useMemo(() => Math.max(...logs.map((l) => l.duration_ms), 1), [logs]);

    return (
        <div className="min-w-0 rounded-lg border border-outline-variant bg-surface-container-low p-4">
            <div className="flex flex-wrap items-center justify-between gap-2 mb-3">
        <span
            className="text-[10px] font-bold text-on-surface-variant uppercase tracking-wider flex items-center gap-1.5">
          <BarChart2 className="h-3.5 w-3.5"/>
          Tool Call Timeline
        </span>
                <div className="flex flex-wrap items-center gap-2 text-xs text-on-surface-variant/70">
                    <span className="flex items-center gap-1"><span
                        className="inline-block w-3 h-2 rounded-sm bg-blue-400/80"/> &lt;10s</span>
                    <span className="flex items-center gap-1"><span
                        className="inline-block w-3 h-2 rounded-sm bg-amber-400/80"/> 10–60s</span>
                    <span className="flex items-center gap-1"><span
                        className="inline-block w-3 h-2 rounded-sm bg-red-400/80"/> &gt;60s</span>
                </div>
            </div>
            <div className="space-y-1">
                {logs.map((log) => {
                    const pct = Math.max(0.3, (log.duration_ms / maxMs) * 100);
                    const variant = getDurationVariant(log.duration_ms);
                    const c = DURATION_CLASSES[variant];
                    const toolName = extractToolName(log.tool_calls_json);
                    return (
                        <button
                            key={log.step_index}
                            onClick={() => onStepClick?.(log.step_index)}
                            className="flex items-center gap-2 group w-full text-left cursor-pointer rounded px-1 -mx-1 hover:bg-surface-container transition-colors"
                        >
              <span className="text-xs font-mono text-on-surface-variant/50 w-5 text-right shrink-0">
                {log.step_index}
              </span>
                            <div
                                className="min-w-0 flex-1 relative h-2.5 bg-surface-container rounded-full overflow-hidden">
                                <div
                                    className={`${c.bg} opacity-60 group-hover:opacity-90 h-full rounded-full transition-opacity`}
                                    style={{width: `${pct}%`}}
                                />
                            </div>
                            <span className={`text-xs font-mono ${c.text} w-14 text-right shrink-0`}>
                {formatDuration(log.duration_ms)}
              </span>
                            <span
                                className={`text-xs font-mono ${toolName ? toolTextColor(toolName) : "text-on-surface-variant"} w-44 truncate shrink-0`}>
                {toolName || log.input_type || "text response"}
              </span>
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

// ── Tool breakdown ───────────────────────────────────────────────────────────

function ToolBreakdown({logs}: { logs: AgentLoopLog[] }) {
    const stats = useMemo(() => buildToolStats(logs), [logs]);
    if (stats.length === 0) return null;

    return (
        <div className="min-w-0 rounded-lg border border-outline-variant bg-surface-container-low p-4">
      <span className="text-[10px] font-bold text-on-surface-variant uppercase tracking-wider block mb-3">
        Tool Call Summary
      </span>
            <div className="grid grid-cols-1 gap-1.5">
                {stats.map((s) => {
                    const variant = getDurationVariant(s.totalMs);
                    const c = DURATION_CLASSES[variant];
                    const tc = toolTextColor(s.tool);
                    const size = computeSize(" ".repeat(s.totalPayloadChars));
                    return (
                        <div
                            key={s.tool}
                            className="flex flex-col gap-0.5 px-2.5 py-1.5 rounded-lg bg-white border border-outline-variant min-w-0"
                        >
                            <div className="flex items-center gap-1.5 min-w-0">
                                <span
                                    className={`font-mono text-[10px] font-semibold ${tc} min-w-0 truncate`}>{s.tool}</span>
                                <span className="text-[9px] text-on-surface-variant shrink-0">×{s.count}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                <span className={`text-[10px] font-bold font-mono shrink-0 ${c.text}`}>
                  {formatDuration(s.totalMs)}
                </span>
                                <span className="text-[9px] text-on-surface-variant whitespace-nowrap">
                  {formatBytes(size.bytes)} · ~{formatNumber(size.estimatedTokens)}
                </span>
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}

interface ToolTraceStat {
    tool: string;
    count: number;
    failures: number;
    totalMs: number;
    totalResultBytes: number;
}

function buildTraceStats(traces: AgentToolCallTrace[]): ToolTraceStat[] {
    const map = new Map<string, ToolTraceStat>();
    for (const trace of traces) {
        const existing = map.get(trace.tool_name) ?? {
            tool: trace.tool_name,
            count: 0,
            failures: 0,
            totalMs: 0,
            totalResultBytes: 0,
        };
        existing.count++;
        existing.failures += trace.error_message ? 1 : 0;
        existing.totalMs += trace.duration_ms || 0;
        existing.totalResultBytes += trace.result_bytes || 0;
        map.set(trace.tool_name, existing);
    }
    return Array.from(map.values()).sort((a, b) => b.totalMs - a.totalMs);
}

function ToolTraceSummary({
                              traces,
                              collapsed,
                              onToggle,
                          }: {
    traces: AgentToolCallTrace[];
    collapsed: boolean;
    onToggle: () => void;
}) {
    const stats = useMemo(() => buildTraceStats(traces), [traces]);
    if (traces.length === 0) return null;

    return (
        <div className="min-w-0 rounded-lg border border-outline-variant bg-surface-container-low">
            <button
                onClick={onToggle}
                className="w-full px-4 py-3 flex items-center justify-between gap-2 text-left hover:bg-surface-container transition rounded-lg"
            >
        <span className="text-[10px] font-bold text-on-surface-variant uppercase tracking-wider">
          Normalized Tool Calls
          <span className="ml-2 text-on-surface-variant/70 normal-case font-normal">
            {traces.length} calls · max concurrency {actualMaxConcurrency(traces)}
          </span>
        </span>
                {collapsed ? <ChevronRight className="h-3.5 w-3.5 shrink-0"/> :
                    <ChevronDown className="h-3.5 w-3.5 shrink-0"/>}
            </button>
            {!collapsed && (
                <div className="px-4 pb-4">
                    <div className="grid grid-cols-2 gap-2 mb-3">
                        <div className="rounded bg-white border border-outline-variant px-2.5 py-2">
                            <div className="text-[9px] uppercase tracking-wider text-on-surface-variant font-bold">Max
                                Concurrency
                            </div>
                            <div className="text-lg font-black text-primary">{actualMaxConcurrency(traces)}</div>
                        </div>
                        <div className="rounded bg-white border border-outline-variant px-2.5 py-2">
                            <div
                                className="text-[9px] uppercase tracking-wider text-on-surface-variant font-bold">Calls
                            </div>
                            <div className="text-lg font-black text-amber-600">{traces.length}</div>
                        </div>
                    </div>
                    <div className="space-y-1.5">
                        {stats.map((stat) => {
                            const variant = getDurationVariant(stat.totalMs);
                            return (
                                <div key={stat.tool}
                                     className="rounded-lg bg-white border border-outline-variant px-2.5 py-1.5">
                                    <div className="flex items-center justify-between gap-2">
                    <span className={`font-mono text-[10px] font-semibold truncate ${toolTextColor(stat.tool)}`}>
                      {stat.tool}
                    </span>
                                        <span className="text-[9px] text-on-surface-variant shrink-0">
                      x{stat.count}{stat.failures ? ` / ${stat.failures} err` : ""}
                    </span>
                                    </div>
                                    <div
                                        className="flex items-center justify-between gap-2 text-[9px] text-on-surface-variant">
                                        <span
                                            className={DURATION_CLASSES[variant].text}>{formatDuration(stat.totalMs)}</span>
                                        <span>{formatBytes(stat.totalResultBytes)}</span>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}
        </div>
    );
}

function ToolTraceCard({trace}: { trace: AgentToolCallTrace }) {
    const [expanded, setExpanded] = useState(false);
    const variant = getDurationVariant(trace.duration_ms);
    const c = DURATION_CLASSES[variant];
    const isFailed = Boolean(trace.error_message);

    return (
        <div className={`rounded-lg border bg-white overflow-hidden ${isFailed ? "border-red-200" : c.border}`}>
            <button
                onClick={() => setExpanded((value) => !value)}
                className="w-full px-4 py-2.5 flex items-center justify-between gap-3 text-left hover:bg-surface-container-low transition"
            >
                <div className="min-w-0 flex items-center gap-2">
          <span
              className="h-5 w-5 rounded bg-surface-container border border-outline-variant flex items-center justify-center text-[10px] font-bold text-on-surface-variant shrink-0 font-mono">
            {trace.step_index}.{trace.call_index + 1}
          </span>
                    <span className={`font-mono text-xs font-semibold truncate ${toolTextColor(trace.tool_name)}`}>
            {trace.tool_name}
          </span>
                    <span
                        className="px-1.5 py-0.5 rounded text-[10px] font-bold bg-surface-container text-on-surface-variant border border-outline-variant shrink-0">
            {trace.tool_kind || "tool"}
          </span>
                    {isFailed && (
                        <span
                            className="px-1.5 py-0.5 rounded text-[10px] font-bold bg-red-50 text-red-700 border border-red-200 shrink-0">
              error
            </span>
                    )}
                </div>
                <div className="flex items-center gap-2 shrink-0">
          <span className="text-[10px] text-on-surface-variant hidden md:inline">
            q {formatDuration(trace.queue_wait_ms)} · lock {formatDuration(trace.lock_wait_ms)}
          </span>
                    <span className={`px-2 py-0.5 rounded-full border text-[10px] font-bold font-mono ${c.badge}`}>
            {formatDuration(trace.duration_ms)}
          </span>
                    {expanded ? <ChevronDown className="h-3.5 w-3.5"/> : <ChevronRight className="h-3.5 w-3.5"/>}
                </div>
            </button>
            {expanded && (
                <div className="border-t border-outline-variant/50 px-4 py-4 space-y-4">
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-2 text-[10px]">
                        <div className="rounded border border-outline-variant bg-surface-container-low px-2 py-1">
                            <div className="font-bold uppercase text-on-surface-variant">Batch</div>
                            <div className="font-mono">{trace.batch_size} calls / limit {trace.parallel_limit}</div>
                        </div>
                        <div className="rounded border border-outline-variant bg-surface-container-low px-2 py-1">
                            <div className="font-bold uppercase text-on-surface-variant">Call ID</div>
                            <div className="font-mono truncate">{trace.call_id || "n/a"}</div>
                        </div>
                        <div className="rounded border border-outline-variant bg-surface-container-low px-2 py-1">
                            <div className="font-bold uppercase text-on-surface-variant">Result Size</div>
                            <div className="font-mono">{formatBytes(trace.result_bytes)} ·
                                ~{formatNumber(trace.estimated_result_tokens)} tok
                            </div>
                        </div>
                        <div className="rounded border border-outline-variant bg-surface-container-low px-2 py-1">
                            <div className="font-bold uppercase text-on-surface-variant">Lock Key</div>
                            <div className="font-mono truncate">{trace.lock_key || "none"}</div>
                        </div>
                    </div>
                    {isFailed && (
                        <div className="rounded-lg bg-red-50 border border-red-200 p-3 text-[11px] text-red-700">
                            {trace.error_message}
                        </div>
                    )}
                    {trace.tool_kind === "read_only" && trace.tool_name !== "context_status" && (
                        <Link
                            href={`/debug/tools?tool=${encodeURIComponent(trace.tool_name)}&args=${encodeURIComponent(trace.args_json)}`}
                            className="inline-flex items-center gap-1 text-[11px] font-semibold text-primary hover:underline"
                        >
                            <ArrowRightLeft className="h-3 w-3"/>
                            Replay in tool console
                        </Link>
                    )}
                    <div className="grid min-w-0 grid-cols-1 gap-3 2xl:grid-cols-2">
                        <PreBox
                            label="Args"
                            labelColor="text-amber-700"
                            labelIcon={<Database className="h-3 w-3"/>}
                            content={tryFormatJSON(trace.args_json)}
                            rawContent={trace.args_json}
                            bgClass="bg-amber-50"
                            borderClass="border-amber-200"
                            textClass="text-amber-900"
                        />
                        <PreBox
                            label="Result"
                            labelColor="text-sky-700"
                            labelIcon={<ArrowRightLeft className="h-3 w-3"/>}
                            content={tryFormatJSON(trace.result_json)}
                            rawContent={trace.result_json}
                            bgClass="bg-sky-50"
                            borderClass="border-sky-200"
                            textClass="text-sky-900"
                        />
                    </div>
                </div>
            )}
        </div>
    );
}

function ToolTraceDetails({
                              traces,
                              collapsed,
                              onToggle,
                          }: {
    traces: AgentToolCallTrace[];
    collapsed: boolean;
    onToggle: () => void;
}) {
    const [toolFilter, setToolFilter] = useState("");
    const [errorOnly, setErrorOnly] = useState(false);
    const visible = useMemo(() => {
        const q = toolFilter.trim().toLowerCase();
        return traces.filter((trace) => {
            if (errorOnly && !trace.error_message) return false;
            if (!q) return true;
            return (
                trace.tool_name.toLowerCase().includes(q) ||
                trace.args_json.toLowerCase().includes(q) ||
                trace.result_json.toLowerCase().includes(q)
            );
        });
    }, [errorOnly, toolFilter, traces]);

    if (traces.length === 0) return null;

    return (
        <div className="space-y-2">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-outline-variant pb-2">
                <button
                    onClick={onToggle}
                    className="flex items-center gap-1.5 text-[10px] font-bold text-on-surface-variant uppercase tracking-wider hover:text-on-surface transition"
                >
                    {collapsed ? <ChevronRight className="h-3.5 w-3.5"/> : <ChevronDown className="h-3.5 w-3.5"/>}
                    Tool Calls Trace (Normalized)
                    <span className="ml-2 text-on-surface-variant/70 normal-case font-normal">
            {traces.length} calls · actual max concurrency {actualMaxConcurrency(traces)}
          </span>
                </button>
                {!collapsed && (
                    <div className="flex items-center gap-2">
                        <input
                            value={toolFilter}
                            onChange={(event) => setToolFilter(event.target.value)}
                            placeholder="Filter tool/args/result..."
                            className="h-7 w-52 rounded border border-outline-variant bg-background px-2 text-[11px] focus:outline-none focus:ring-2 focus:ring-primary/30"
                        />
                        <button
                            onClick={() => setErrorOnly((value) => !value)}
                            className={`h-7 rounded border px-2 text-[10px] font-bold ${
                                errorOnly ? "border-red-200 bg-red-50 text-red-700" : "border-outline-variant bg-surface-container text-on-surface-variant"
                            }`}
                        >
                            {errorOnly ? "Errors" : "All"}
                        </button>
                    </div>
                )}
            </div>
            {!collapsed && (
                visible.length === 0 ? (
                    <div
                        className="rounded-lg border border-dashed border-outline-variant p-4 text-center text-xs text-on-surface-variant">
                        No tool calls match the current filter.
                    </div>
                ) : (
                    visible.map((trace) => <ToolTraceCard key={trace.id} trace={trace}/>)
                )
            )}
        </div>
    );
}

// ── Step card ────────────────────────────────────────────────────────────────

function StepCard({
                      log,
                      isExpanded,
                      onToggle,
                  }: {
    log: AgentLoopLog;
    isExpanded: boolean;
    onToggle: () => void;
}) {
    const variant = getDurationVariant(log.duration_ms);
    const c = DURATION_CLASSES[variant];
    const toolName = extractToolName(log.tool_calls_json);
    const hasToolCalls = !!(log.tool_calls_json && log.tool_calls_json !== "[]");
    const hasToolResults = !!(log.tool_results_json && log.tool_results_json !== "[]");
    const hasReasoning = !!log.reasoning_text;
    const hasAssistant = !!log.assistant_text;
    const isTimeout = log.duration_ms > 60000;
    const isFinal = !hasToolCalls && hasAssistant;

    return (
        <div id={`step-${log.step_index}`}
             className={`min-w-0 rounded-lg border ${c.border} bg-white overflow-hidden transition-colors hover:border-primary/30`}>
            <button
                onClick={onToggle}
                className="w-full px-4 py-2.5 flex items-center justify-between hover:bg-surface-container-low transition text-left"
            >
                <div className="flex items-center gap-2.5 min-w-0">
          <span
              className="h-5 w-5 rounded bg-surface-container border border-outline-variant flex items-center justify-center text-xs font-bold text-on-surface-variant shrink-0 font-mono">
            {log.step_index}
          </span>
                    {toolName ? (
                        <span className={`font-mono text-xs font-semibold shrink-0 ${toolTextColor(toolName)}`}>
              {toolName}
            </span>
                    ) : (
                        <span className="text-xs font-semibold text-on-surface-variant shrink-0">
              {log.input_type || "text response"}
            </span>
                    )}
                    {isTimeout && (
                        <span
                            className="px-1.5 py-0.5 rounded text-xs font-bold bg-red-50 text-red-700 border border-red-200 shrink-0 flex items-center gap-1">
              <AlertTriangle className="h-2.5 w-2.5"/> TIMEOUT
            </span>
                    )}
                    {isFinal && (
                        <span
                            className="px-1.5 py-0.5 rounded text-xs font-bold bg-emerald-50 text-emerald-700 border border-emerald-200 shrink-0 flex items-center gap-1">
              <CheckCircle className="h-2.5 w-2.5"/> Final
            </span>
                    )}
                </div>

                <div className="flex items-center gap-3 shrink-0 ml-3">
          <span
              className="px-2 py-0.5 rounded-full border border-outline-variant bg-surface-container-low text-on-surface-variant text-[10px] font-bold hidden lg:inline">
            input {formatNumber(Math.round((log.input_content?.length || 0) / 4))}
          </span>
                    <span
                        className="px-2 py-0.5 rounded-full border border-amber-200 bg-amber-50 text-amber-800 text-[10px] font-bold hidden lg:inline">
            tool call {formatNumber(Math.round((log.tool_calls_json?.length || 0) / 4))}
          </span>
                    <span
                        className={`px-2 py-0.5 rounded-full border text-[10px] font-bold font-mono flex items-center gap-1 ${c.badge}`}>
            <Clock className="h-3 w-3 stroke-[2]"/>
                        {formatDuration(log.duration_ms)}
          </span>
                    <span className="text-[10px] text-on-surface-variant/60 font-mono hidden md:inline">
            {new Date(log.created_at).toLocaleTimeString([], {
                hour: "2-digit",
                minute: "2-digit",
                second: "2-digit",
            })}
          </span>
                    {isExpanded ? (
                        <ChevronDown className="h-3.5 w-3.5 text-on-surface-variant/50"/>
                    ) : (
                        <ChevronRight className="h-3.5 w-3.5 text-on-surface-variant/50"/>
                    )}
                </div>
            </button>

            {isExpanded && (
                <div className="min-w-0 border-t border-outline-variant/50 px-4 py-4 space-y-4 text-xs">
                    {isTimeout && (
                        <div
                            className="rounded-lg bg-red-50 border border-red-200 p-3 text-[11px] text-red-700 leading-relaxed flex items-start gap-2">
                            <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-0.5"/>
                            <span>
                This step took &gt;60s. Often caused by a blocking UI wait, idle timeout, or a very
                large LLM context window. Check the tool result for timeout/wait clues.
              </span>
                        </div>
                    )}

                    <PreBox
                        label="Handoff Input"
                        content={tryFormatJSON(log.input_content)}
                        rawContent={log.input_content}
                    />

                    {hasReasoning && (
                        <PreBox
                            label="LLM Reasoning"
                            labelColor="text-violet-700"
                            labelIcon={<Cpu className="h-3 w-3"/>}
                            content={log.reasoning_text}
                            bgClass="bg-violet-50"
                            borderClass="border-violet-200"
                            textClass="text-violet-900"
                        />
                    )}

                    {hasAssistant && (
                        <PreBox
                            label="Assistant Response"
                            content={log.assistant_text}
                            bgClass="bg-surface-container-low"
                            borderClass="border-outline-variant"
                            textClass="text-on-surface"
                        />
                    )}

                    {hasToolCalls && (
                        <div className="grid min-w-0 grid-cols-1 gap-3 2xl:grid-cols-2">
                            <PreBox
                                label="Tool Call"
                                labelColor="text-amber-700"
                                labelIcon={<Database className="h-3 w-3"/>}
                                content={tryFormatJSON(log.tool_calls_json)}
                                rawContent={log.tool_calls_json}
                                bgClass="bg-amber-50"
                                borderClass="border-amber-200"
                                textClass="text-amber-900"
                            />
                            <PreBox
                                label="Tool Result"
                                labelColor="text-sky-700"
                                labelIcon={<ArrowRightLeft className="h-3 w-3"/>}
                                content={hasToolResults ? tryFormatJSON(log.tool_results_json) : "No results returned"}
                                rawContent={log.tool_results_json}
                                bgClass="bg-sky-50"
                                borderClass="border-sky-200"
                                textClass="text-sky-900"
                            />
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

// ── Main page ────────────────────────────────────────────────────────────────

const SESSION_TYPES = ["collector", "dashboard", "project_compile", "concept_compile", "person_enrich"];

export default function DebugPage() {
    const [sessions, setSessions] = useState<AgentSession[]>([]);
    const [selectedSessionId, setSelectedSessionId] = useState<number | null>(null);
    const [logs, setLogs] = useState<AgentLoopLog[]>([]);
    const [toolCalls, setToolCalls] = useState<AgentToolCallTrace[]>([]);
    const [askSessionLink, setAskSessionLink] = useState<AskSessionDebugLink | null>(null);
    const [loadingSessions, setLoadingSessions] = useState(false);
    const [loadingLogs, setLoadingLogs] = useState(false);
    const [logsError, setLogsError] = useState<string | null>(null);
    const [expandedSteps, setExpandedSteps] = useState<Record<number, boolean>>({});
    const [showSlowOnly, setShowSlowOnly] = useState(false);
    const [manualSessionId, setManualSessionId] = useState("");
    const [entityType, setEntityType] = useState("concept_compile");
    const [entityName, setEntityName] = useState("");
    const [cancellingId, setCancellingId] = useState<number | null>(null);
    const [purgingId, setPurgingId] = useState<number | null>(null);
    const [systemInfo, setSystemInfo] = useState<DebugSystemInfo | null>(null);
    const [traceSummaryCollapsed, setTraceSummaryCollapsed] = useState(true);
    const [traceDetailsCollapsed, setTraceDetailsCollapsed] = useState(true);

    const fetchLogs = useCallback(async (id: number) => {
        setLoadingLogs(true);
        setLogsError(null);
        try {
            const res = await fetch(`/api/agents/logs?sessionId=${id}`);
            if (!res.ok) throw new Error(`Failed to fetch logs for session ${id}`);
            const data = normalizeLogsResponse<AgentLoopLog, AgentToolCallTrace>(await res.json());
            setLogs(data.loops);
            setToolCalls(data.tool_calls);
            setAskSessionLink((data.ask_session as AskSessionDebugLink | null | undefined) ?? null);
            const initial: Record<number, boolean> = {};
            data.loops.forEach((l, idx) => {
                initial[l.step_index] = idx < 2 || l.duration_ms > 10000;
            });
            setExpandedSteps(initial);
        } catch (e) {
            setLogsError(e instanceof Error ? e.message : String(e));
            setLogs([]);
            setToolCalls([]);
            setAskSessionLink(null);
        } finally {
            setLoadingLogs(false);
        }
    }, []);

    const fetchSessions = useCallback(async () => {
        setLoadingSessions(true);
        try {
            const res = await fetch("/api/agents/sessions");
            if (!res.ok) throw new Error("Failed to fetch sessions");
            const data = await res.json();
            setSessions(data);
            if (data.length > 0) {
                setSelectedSessionId((current) => current ?? data[0].id);
            }
        } catch (e) {
            console.error(e);
        } finally {
            setLoadingSessions(false);
        }
    }, []);

    const fetchLogsByEntity = useCallback(async () => {
        if (!entityName.trim()) return;
        setLoadingLogs(true);
        setLogsError(null);
        setSelectedSessionId(null);
        try {
            const res = await fetch(
                `/api/agents/logs?type=${encodeURIComponent(entityType)}&entityId=${encodeURIComponent(entityName.trim())}`
            );
            if (!res.ok) throw new Error("No session found for that entity");
            const data = normalizeLogsResponse<AgentLoopLog, AgentToolCallTrace>(await res.json());
            setLogs(data.loops);
            setToolCalls(data.tool_calls);
            setAskSessionLink((data.ask_session as AskSessionDebugLink | null | undefined) ?? null);
            const initial: Record<number, boolean> = {};
            data.loops.forEach((l, idx) => {
                initial[l.step_index] = idx < 2 || l.duration_ms > 10000;
            });
            setExpandedSteps(initial);
        } catch (e) {
            setLogsError(e instanceof Error ? e.message : String(e));
            setLogs([]);
            setToolCalls([]);
            setAskSessionLink(null);
        } finally {
            setLoadingLogs(false);
        }
    }, [entityName, entityType]);

    useEffect(() => {
        void Promise.resolve().then(() => fetchSessions());
    }, [fetchSessions]);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                const res = await fetch("/api/debug/system");
                if (!res.ok) return;
                const info = (await res.json()) as DebugSystemInfo;
                if (!cancelled) setSystemInfo(info);
            } catch {
                // best-effort; debug-only system info, no error UI needed
            }
        })();
        return () => {
            cancelled = true;
        };
    }, []);

    const handleCancelSession = async (id: number) => {
        if (!confirm(`Are you sure you want to cancel Run #${id}?`)) return;
        setCancellingId(id);
        try {
            const res = await fetch(`/api/agents/runs/${id}/cancel`, {
                method: "POST",
            });
            if (!res.ok) {
                const data = await res.json().catch(() => ({}));
                throw new Error(data.error || "Failed to cancel run");
            }
            // Refresh sessions and logs
            await fetchSessions();
            await fetchLogs(id);
        } catch (e) {
            console.error(e);
            alert(e instanceof Error ? e.message : "Failed to cancel run");
        } finally {
            setCancellingId(null);
        }
    };

    const handlePurgeSession = async (id: number) => {
        if (!confirm(`Purge raw debug data for Run #${id}? Saved Ask Session answers are preserved.`)) return;
        setPurgingId(id);
        try {
            const res = await fetch(`/api/agents/runs/${id}`, {
                method: "DELETE",
            });
            if (!res.ok) {
                const data = await res.json().catch(() => ({}));
                throw new Error(data.error || "Failed to purge run");
            }
            setLogs([]);
            setToolCalls([]);
            setAskSessionLink(null);
            setSelectedSessionId(null);
            await fetchSessions();
        } catch (e) {
            console.error(e);
            alert(e instanceof Error ? e.message : "Failed to purge run");
        } finally {
            setPurgingId(null);
        }
    };

    useEffect(() => {
        if (selectedSessionId === null) return;
        void Promise.resolve().then(() => fetchLogs(selectedSessionId));
    }, [fetchLogs, selectedSessionId]);

    const handleManualLoad = (e: React.FormEvent) => {
        e.preventDefault();
        const id = parseInt(manualSessionId.trim(), 10);
        if (Number.isFinite(id) && id > 0) setSelectedSessionId(id);
    };

    const handleEntitySearch = (e: React.FormEvent) => {
        e.preventDefault();
        fetchLogsByEntity();
    };

    const selectedSession = sessions.find((s) => s.id === selectedSessionId);
    const hasLogs = logs.length > 0 || toolCalls.length > 0;
    const totalMs = useMemo(() => logs.reduce((a, l) => a + (l.duration_ms || 0), 0), [logs]);
    const tokenTotals = useMemo(
        () =>
            logs.reduce(
                (a, l) => ({
                    estimatedInput: a.estimatedInput + (l.estimated_input_tokens || 0),
                    estimatedOutput: a.estimatedOutput + (l.estimated_output_tokens || 0),
                    estimatedToolResults: a.estimatedToolResults + (l.estimated_tool_result_tokens || 0),
                    modelInput: a.modelInput + (l.model_input_tokens || 0),
                    modelOutput: a.modelOutput + (l.model_output_tokens || 0),
                    modelTotal: a.modelTotal + (l.model_total_tokens || 0),
                }),
                {
                    estimatedInput: 0,
                    estimatedOutput: 0,
                    estimatedToolResults: 0,
                    modelInput: 0,
                    modelOutput: 0,
                    modelTotal: 0,
                }
            ),
        [logs]
    );
    const estimatedInputTokens = useMemo(
        () => logs.reduce((sum, l) => sum + Math.round((l.input_content?.length || 0) / 4), 0),
        [logs]
    );
    const estimatedCallTokens = useMemo(
        () => logs.reduce((sum, l) => sum + Math.round((l.tool_calls_json?.length || 0) / 4), 0),
        [logs]
    );
    const totalPayload = useMemo(() => {
        let totalChars = 0;
        for (const l of logs) {
            totalChars += l.input_content.length;
            totalChars += l.tool_calls_json.length;
        }
        for (const trace of toolCalls) {
            totalChars += trace.args_json.length;
            totalChars += trace.result_json.length;
        }
        return computeSize(" ".repeat(totalChars));
    }, [logs, toolCalls]);
    const slowestStep = useMemo(
        () => logs.reduce<AgentLoopLog | null>((a, l) => (!a || l.duration_ms > a.duration_ms ? l : a), null),
        [logs]
    );
    const toolCallCount = useMemo(
        () => toolCalls.length || logs.filter((l) => l.tool_calls_json && l.tool_calls_json !== "[]").length,
        [logs, toolCalls]
    );
    const maxToolConcurrency = useMemo(() => actualMaxConcurrency(toolCalls), [toolCalls]);
    const visibleLogs = useMemo(
        () => (showSlowOnly ? logs.filter((l) => l.duration_ms > 10000) : logs),
        [logs, showSlowOnly]
    );
    const showingLogs = hasLogs || loadingLogs || logsError;

    return (
        <main className="h-screen bg-background text-on-surface flex flex-col font-sans overflow-hidden">

            {/* Header */}
            <header
                className="shrink-0 border-b border-outline-variant bg-white/90 backdrop-blur px-5 py-3 flex items-center justify-between z-20">
                <div className="flex items-center gap-3">
                    <Link
                        href="/home"
                        className="p-1.5 rounded border border-outline-variant bg-surface-container text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high transition"
                    >
                        <ArrowLeft className="h-4 w-4"/>
                    </Link>
                    <div>
                        <h1 className="text-sm font-bold text-primary leading-tight tracking-tight">
                            Agent Execution Debugger
                        </h1>
                        <p className="text-[10px] text-on-surface-variant leading-tight">
                            Inspect multi-turn LLM reasoning, step durations, and tool inputs/outputs
                        </p>
                    </div>
                </div>

                <div className="flex items-center gap-2">
                    {/* Entity search */}
                    <form
                        onSubmit={handleEntitySearch}
                        className="flex items-center gap-1.5 bg-surface-container-low border border-outline-variant rounded px-3 py-1.5"
                    >
                        <select
                            value={entityType}
                            onChange={(e) => setEntityType(e.target.value)}
                            className="bg-transparent text-xs text-on-surface-variant focus:outline-none pr-1 border-r border-outline-variant mr-1.5"
                        >
                            {SESSION_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
                        </select>
                        <input
                            type="text"
                            placeholder="Entity slug…"
                            value={entityName}
                            onChange={(e) => setEntityName(e.target.value)}
                            className="bg-transparent text-xs text-on-surface placeholder-on-surface-variant/50 focus:outline-none w-36"
                        />
                        <button type="submit" className="ml-1 text-primary hover:text-primary/70 transition">
                            <Search className="h-3.5 w-3.5"/>
                        </button>
                    </form>

                    <span className="text-outline-variant text-xs">or</span>

                    {/* Session ID */}
                    <form
                        onSubmit={handleManualLoad}
                        className="flex items-center gap-1.5 bg-surface-container-low border border-outline-variant rounded px-3 py-1.5"
                    >
                        <span className="text-[10px] text-on-surface-variant font-mono">#</span>
                        <input
                            type="number"
                            placeholder="Session ID…"
                            value={manualSessionId}
                            onChange={(e) => setManualSessionId(e.target.value)}
                            className="bg-transparent text-xs text-on-surface placeholder-on-surface-variant/50 focus:outline-none w-24"
                        />
                        <button
                            type="submit"
                            className="px-2 py-0.5 rounded bg-primary hover:bg-primary/80 text-primary-foreground text-[10px] font-bold transition"
                        >
                            Load
                        </button>
                    </form>
                </div>
            </header>

            {/* Two-panel body — each panel scrolls independently */}
            <div className="flex flex-1 min-h-0 overflow-hidden">

                {/* Sidebar */}
                <aside
                    className="w-72 shrink-0 border-r border-outline-variant bg-surface-container-lowest flex flex-col overflow-y-auto">
                    <div
                        className="px-4 py-2.5 border-b border-outline-variant flex justify-between items-center sticky top-0 bg-surface-container-lowest z-10">
            <span className="text-[10px] font-bold text-on-surface-variant uppercase tracking-wider">
              Recent Runs
            </span>
                        <button
                            onClick={fetchSessions}
                            disabled={loadingSessions}
                            className="text-[10px] font-semibold text-primary hover:text-primary/70 transition"
                        >
                            Refresh
                        </button>
                    </div>

                    {loadingSessions ? (
                        <div
                            className="flex-1 flex items-center justify-center p-8 text-on-surface-variant text-xs gap-2">
                            <Activity className="h-4 w-4 animate-spin text-primary"/>
                            Loading…
                        </div>
                    ) : sessions.length === 0 ? (
                        <div className="p-8 text-center text-on-surface-variant text-xs">No sessions found.</div>
                    ) : (
                        <div className="divide-y divide-outline-variant/40">
                            {sessions.map((s) => (
                                <button
                                    key={s.id}
                                    onClick={() => setSelectedSessionId(s.id)}
                                    className={`w-full px-4 py-3 text-left flex flex-col gap-1.5 border-l-2 transition ${
                                        selectedSessionId === s.id
                                            ? "bg-primary-fixed/40 border-l-primary"
                                            : "border-l-transparent hover:bg-surface-container"
                                    }`}
                                >
                                    <div className="flex justify-between items-start">
                                        <span
                                            className="text-[10px] font-bold text-on-surface-variant">Run #{s.id}</span>
                                        <span
                                            className={`px-1.5 py-0.5 rounded text-xs font-bold border ${sessionTypeClass(s.session_type)}`}>
                      {s.session_type}
                    </span>
                                    </div>
                                    <div
                                        className="text-xs font-semibold text-on-surface truncate">{sessionTitle(s)}</div>
                                    <div className="flex justify-between text-[10px] text-on-surface-variant/70">
                    <span>
                      {new Date(s.created_at).toLocaleDateString()} at{" "}
                        {new Date(s.created_at).toLocaleTimeString([], {hour: "2-digit", minute: "2-digit"})}
                    </span>
                                        <span className="capitalize">{s.status}</span>
                                    </div>
                                </button>
                            ))}
                        </div>
                    )}
                </aside>

                {/* Main content */}
                <section className="min-w-0 flex-1 flex flex-col bg-background min-h-0 overflow-hidden">
                    {!showingLogs ? (
                        <div
                            className="flex-1 flex flex-col items-center justify-center text-center p-8 max-w-lg mx-auto">
                            <Terminal className="h-14 w-14 text-on-surface-variant/20 mb-4 stroke-[1.5]"/>
                            <h3 className="text-base font-semibold text-on-surface mb-2">No Session Selected</h3>
                            <p className="text-xs text-on-surface-variant leading-6 mb-5">
                                Pick a session from the sidebar, search by entity slug, or enter a session ID to
                                inspect its step logs.
                            </p>
                            <div className="flex flex-wrap justify-center gap-2">
                                {sessions.slice(0, 3).map((s) => (
                                    <button
                                        key={s.id}
                                        onClick={() => setSelectedSessionId(s.id)}
                                        className="px-3 py-1.5 rounded bg-surface-container-low hover:bg-surface-container-high border border-outline-variant text-xs font-semibold text-on-surface transition"
                                    >
                                        Run #{s.id} · {sessionTitle(s)}
                                    </button>
                                ))}
                            </div>
                        </div>
                    ) : (
                        <div className="min-w-0 flex-1 flex flex-col min-h-0">

                            {/* Session overview — pinned top strip */}
                            {(selectedSession || hasLogs) && (
                                <div className="shrink-0 px-5 pt-5 pb-4">
                                    <div
                                        className="min-w-0 overflow-hidden rounded-lg border border-outline-variant bg-white px-5 py-4 space-y-3">
                                        {/* Title row */}
                                        <div className="flex items-center justify-between flex-wrap gap-2">
                                            <div className="flex items-center gap-2 flex-wrap">
                                                <h2 className="text-sm font-bold text-on-surface">
                                                    {selectedSession
                                                        ? `Run #${selectedSessionId} · ${selectedSession.session_type}`
                                                        : `${entityType} · ${entityName}`}
                                                </h2>
                                                {selectedSession && (
                                                    <span
                                                        className={`px-2 py-0.5 rounded-full text-xs font-bold border ${sessionTypeClass(selectedSession.session_type)}`}>
                            {selectedSession.status}
                          </span>
                                                )}
                                                {selectedSession && (
                                                    <span className="text-[11px] text-on-surface-variant ml-1">
                            Entity: <span className="font-semibold text-on-surface">{selectedSession.entity_id}</span>
                                                        {selectedSession.user_message && (
                                                            <>
                                                                {" · "}Prompt:{" "}
                                                                <span className="font-semibold text-on-surface">
                                  &ldquo;{selectedSession.user_message.length > 80
                                                                    ? selectedSession.user_message.slice(0, 77) + "…"
                                                                    : selectedSession.user_message}&rdquo;
                                </span>
                                                            </>
                                                        )}
                                                        {selectedSession.model && (
                                                            <>
                                                                {" · "}Model:{" "}
                                                                <span
                                                                    className="font-semibold text-on-surface">{selectedSession.model}</span>
                                                                {selectedSession.provider && (
                                                                    <span
                                                                        className="text-on-surface-variant/70"> ({selectedSession.provider})</span>
                                                                )}
                                                            </>
                                                        )}
                                                        {" · "}Started {new Date(selectedSession.created_at).toLocaleString()}
                          </span>
                                                )}
                                            </div>
                                            {selectedSession && (selectedSession.status === "running" || selectedSession.status === "queued" || selectedSession.status === "waiting_for_user" || selectedSession.status === "active") && (
                                                <button
                                                    onClick={() => handleCancelSession(selectedSession.id)}
                                                    disabled={cancellingId === selectedSession.id}
                                                    className="px-2.5 py-1 rounded bg-red-50 hover:bg-red-100 text-red-700 border border-red-200 text-xs font-bold transition disabled:opacity-50"
                                                >
                                                    {cancellingId === selectedSession.id ? "Cancelling..." : "Cancel Run"}
                                                </button>
                                            )}
                                            {selectedSession && (
                                                <button
                                                    onClick={() => handlePurgeSession(selectedSession.id)}
                                                    disabled={
                                                        purgingId === selectedSession.id ||
                                                        cancellingId === selectedSession.id ||
                                                        selectedSession.status === "running" ||
                                                        selectedSession.status === "queued" ||
                                                        selectedSession.status === "waiting_for_user" ||
                                                        selectedSession.status === "active"
                                                    }
                                                    className="px-2.5 py-1 rounded bg-surface-container-low hover:bg-surface-container-high text-on-surface-variant border border-outline-variant text-xs font-bold transition disabled:opacity-50"
                                                >
                                                    {purgingId === selectedSession.id ? "Purging..." : "Purge Debug"}
                                                </button>
                                            )}
                                        </div>
                                        {askSessionLink && (
                                            <div
                                                className="flex flex-wrap items-center gap-2 border-t border-outline-variant pt-2 text-[11px] text-on-surface-variant">
                                                <span
                                                    className="font-bold uppercase tracking-wider text-on-surface-variant">Ask Session</span>
                                                <Link
                                                    href={`/sessions/${askSessionLink.slug}`}
                                                    className="font-semibold text-primary underline-offset-2 hover:underline"
                                                >
                                                    {askSessionLink.title}
                                                </Link>
                                                <span>
                          Turn {askSessionLink.turn_index + 1} · Session #{askSessionLink.id} · Turn #{askSessionLink.turn_id}
                        </span>
                                            </div>
                                        )}
                                        {/* System info — process working directory and configured LLM endpoint */}
                                        {systemInfo && (
                                            <div
                                                className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-on-surface-variant border-t border-outline-variant pt-2">
                        <span>
                          Project folder:{" "}
                            <span className="font-mono text-on-surface" title={systemInfo.working_directory}>
                            {systemInfo.working_directory || "n/a"}
                          </span>
                        </span>
                                                {systemInfo.msgvault_db_path && (
                                                    <span>
                            Archive DB:{" "}
                                                        <span className="font-mono text-on-surface"
                                                              title={systemInfo.msgvault_db_path}>
                              {systemInfo.msgvault_db_path}
                            </span>
                          </span>
                                                )}
                                                {systemInfo.model_base_url && (
                                                    <span>
                            Endpoint:{" "}
                                                        <span className="font-mono text-on-surface"
                                                              title={systemInfo.model_base_url}>
                              {systemInfo.model_base_url}
                            </span>
                          </span>
                                                )}
                                            </div>
                                        )}
                                        {/* Metrics row — flex-wrap so it never overflows */}
                                        {hasLogs && (
                                            <div
                                                className="flex flex-wrap items-start gap-x-5 gap-y-2 border-t border-outline-variant pt-3">
                                                <div>
                                                    <div
                                                        className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Steps
                                                    </div>
                                                    <div className="text-xl font-black text-primary">{logs.length}</div>
                                                </div>
                                                <div>
                                                    <div
                                                        className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Total
                                                        Time
                                                    </div>
                                                    <div
                                                        className="text-xl font-black text-on-surface">{formatDuration(totalMs)}</div>
                                                </div>
                                                {slowestStep && (
                                                    <div>
                                                        <div
                                                            className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Slowest
                                                        </div>
                                                        <div
                                                            className={`text-xl font-black ${DURATION_CLASSES[getDurationVariant(slowestStep.duration_ms)].text}`}>
                                                            {formatDuration(slowestStep.duration_ms)}
                                                        </div>
                                                        <div className="text-[10px] text-on-surface-variant">
                                                            {extractToolName(slowestStep.tool_calls_json) ?? "step"} (#{slowestStep.step_index})
                                                        </div>
                                                    </div>
                                                )}
                                                <div>
                                                    <div
                                                        className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Tool
                                                        Calls
                                                    </div>
                                                    <div
                                                        className="text-xl font-black text-amber-600">{toolCallCount}</div>
                                                </div>
                                                {toolCalls.length > 0 && (
                                                    <div>
                                                        <div
                                                            className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Tool
                                                            Concurrency
                                                        </div>
                                                        <div
                                                            className="text-xl font-black text-primary">{maxToolConcurrency}</div>
                                                        <div className="text-[10px] text-on-surface-variant">
                                                            actual max overlap
                                                        </div>
                                                    </div>
                                                )}
                                                <div>
                                                    <div
                                                        className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Model
                                                        Tokens
                                                    </div>
                                                    <div className="text-xl font-black text-primary">
                                                        {tokenTotals.modelTotal > 0 ? formatNumber(tokenTotals.modelTotal) : "n/a"}
                                                    </div>
                                                    <div className="text-[10px] text-on-surface-variant">
                                                        in {formatNumber(tokenTotals.modelInput)} /
                                                        out {formatNumber(tokenTotals.modelOutput)}
                                                    </div>
                                                </div>
                                                <div>
                                                    <div
                                                        className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Est.
                                                        Tokens
                                                    </div>
                                                    <div className="text-xl font-black text-primary">
                                                        {formatNumber(estimatedInputTokens + estimatedCallTokens)}
                                                    </div>
                                                    <div className="text-[10px] text-on-surface-variant">
                                                        in {formatNumber(estimatedInputTokens)} /
                                                        out {formatNumber(estimatedCallTokens)}
                                                    </div>
                                                </div>
                                                <div>
                                                    <div
                                                        className="text-[10px] text-on-surface-variant uppercase tracking-wider font-bold">Total
                                                        Payload
                                                    </div>
                                                    <div
                                                        className="text-xl font-black text-on-surface whitespace-nowrap">
                                                        {formatBytes(totalPayload.bytes)}
                                                    </div>
                                                    <div className="text-[10px] text-on-surface-variant">
                                                        ~{formatNumber(totalPayload.estimatedTokens)} tok
                                                    </div>
                                                </div>
                                            </div>
                                        )}
                                    </div>
                                </div>
                            )}

                            {loadingLogs ? (
                                <div
                                    className="flex-1 flex flex-col items-center justify-center text-on-surface-variant text-sm gap-2">
                                    <Activity className="h-8 w-8 animate-spin text-primary mb-2"/>
                                    Loading execution logs…
                                </div>
                            ) : logsError ? (
                                <div className="px-5 py-4">
                                    <div
                                        className="p-4 border border-red-200 bg-red-50 rounded-lg text-red-700 text-xs flex items-start gap-2">
                                        <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5"/>
                                        {logsError}
                                    </div>
                                </div>
                            ) : logs.length === 0 && toolCalls.length === 0 ? (
                                <div
                                    className="flex-1 flex items-center justify-center m-5 text-on-surface-variant text-sm border border-dashed border-outline-variant rounded-lg">
                                    No steps recorded for this session.
                                </div>
                            ) : (
                                /* Fixed side rail + constrained details column. */
                                <div
                                    className="grid min-h-0 min-w-0 flex-1 grid-cols-[300px_minmax(0,1fr)] overflow-hidden">

                                    {/* Side rail: tool summary + timeline */}
                                    <div
                                        className="min-w-0 overflow-y-auto border-r border-outline-variant bg-surface-container-lowest p-4 space-y-4">
                                        <ToolTraceSummary
                                            traces={toolCalls}
                                            collapsed={traceSummaryCollapsed}
                                            onToggle={() => setTraceSummaryCollapsed((v) => !v)}
                                        />
                                        {toolCalls.length === 0 && <ToolBreakdown logs={logs}/>}
                                        {logs.length > 0 && (
                                            <WaterfallChart
                                                logs={logs}
                                                onStepClick={(stepIndex) => {
                                                    setExpandedSteps((prev) => ({...prev, [stepIndex]: true}));
                                                    setTimeout(() => {
                                                        const panel = document.getElementById("step-details-panel");
                                                        const target = document.getElementById(`step-${stepIndex}`);
                                                        if (panel && target) {
                                                            const panelTop = panel.getBoundingClientRect().top;
                                                            const targetTop = target.getBoundingClientRect().top;
                                                            panel.scrollBy({
                                                                top: targetTop - panelTop - 16,
                                                                behavior: "smooth"
                                                            });
                                                        }
                                                    }, 50);
                                                }}
                                            />
                                        )}
                                    </div>

                                    {/* Details panel: step-by-step trace */}
                                    <div id="step-details-panel" className="min-w-0 overflow-y-auto p-5 space-y-4">
                                        <ToolTraceDetails
                                            traces={toolCalls}
                                            collapsed={traceDetailsCollapsed}
                                            onToggle={() => setTraceDetailsCollapsed((v) => !v)}
                                        />
                                        <div
                                            className="flex flex-wrap items-center justify-between gap-3 border-b border-outline-variant pb-2 mt-12">
                      <span className="text-[10px] font-bold text-on-surface-variant uppercase tracking-wider">
                        Agentic Loop Steps
                          {showSlowOnly && (
                              <span className="ml-2 text-amber-600 normal-case font-normal">
                            ({visibleLogs.length} slow steps)
                          </span>
                          )}
                      </span>
                                            <div
                                                className="flex flex-wrap items-center gap-2 text-[10px] text-on-surface-variant sm:gap-3">
                                                <button
                                                    onClick={() => {
                                                        const e: Record<number, boolean> = {};
                                                        visibleLogs.forEach((l) => (e[l.step_index] = true));
                                                        setExpandedSteps(e);
                                                    }}
                                                    className="hover:text-on-surface transition"
                                                >
                                                    Expand All
                                                </button>
                                                <span className="text-outline-variant">|</span>
                                                <button
                                                    onClick={() => setExpandedSteps({})}
                                                    className="hover:text-on-surface transition"
                                                >
                                                    Collapse All
                                                </button>
                                                <span className="text-outline-variant">|</span>
                                                <button
                                                    onClick={() => setShowSlowOnly((v) => !v)}
                                                    className={`flex items-center gap-1 transition ${
                                                        showSlowOnly ? "text-amber-600" : "hover:text-amber-700"
                                                    }`}
                                                >
                                                    <Filter className="h-3 w-3"/>
                                                    {showSlowOnly ? "Show All" : "Slow Only (>10s)"}
                                                </button>
                                            </div>
                                        </div>

                                        <div className="space-y-2">
                                            {visibleLogs.map((log) => (
                                                <StepCard
                                                    key={log.step_index}
                                                    log={log}
                                                    isExpanded={!!expandedSteps[log.step_index]}
                                                    onToggle={() => {
                                                        setExpandedSteps((prev) => ({
                                                            ...prev,
                                                            [log.step_index]: !prev[log.step_index]
                                                        }));
                                                    }}
                                                />
                                            ))}
                                        </div>
                                    </div>

                                </div>
                            )}
                        </div>
                    )}
                </section>
            </div>
        </main>
    );
}
