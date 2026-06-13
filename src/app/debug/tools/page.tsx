"use client";

import Link from "next/link";
import {type ReactNode, useEffect, useMemo, useState} from "react";
import {
    Activity,
    AlertTriangle,
    ArrowLeft,
    CheckCircle,
    Clock,
    Database,
    Mail,
    Play,
    RotateCcw,
    Terminal,
    WandSparkles,
} from "lucide-react";

interface ToolSchema {
    type: string;
    name: string;
    description: string;
    parameters: JSONSchema | string;
}

interface JSONSchema {
    type?: string;
    properties?: Record<string, JSONSchema>;
    required?: string[];
    description?: string;
    enum?: string[];
    items?: JSONSchema;
}

interface InvokeEnvelope {
    tool: string;
    status: number;
    duration_ms: number;
    response_size_bytes: number;
    estimated_tokens: number;
    data?: unknown;
    error?: unknown;
}

interface RecentInvocation {
    id: number;
    tool: string;
    params: Record<string, unknown>;
    status: number;
    duration_ms: number;
    estimated_tokens: number;
}

interface MessageRecord {
    message_id?: number;
    date?: string;
    subject?: string;
    sender_canonical_name?: string;
    sender_primary_email?: string;
    from_name?: string;
    from_email?: string;
    snippet?: string;
    body_text?: string;
    direction?: string;
}

type FieldValue = string | boolean;

const TOOL_SAMPLES: Record<string, { label: string; params: Record<string, unknown> }> = {
    cluster_messages_by_subject: {
        label: "Cluster three related messages into three groups.",
        params: {message_ids: [607, 592, 599], k: 3},
    },
    detect_gaps: {
        label: "Find chronological gaps around a small message set.",
        params: {message_ids: [607, 592, 599], mode: "chronological", min_severity: "medium", max_gaps: 3},
    },
    detect_gaps_with_results: {
        label: "Find thematic gaps and include candidate search results.",
        params: {message_ids: [607, 592, 599], mode: "thematic", min_severity: "medium", max_gaps: 3},
    },
    find_bridges_between: {
        label: "Look for social bridge messages between two people.",
        params: {person_a_id: 1, person_b_id: 2, limit: 5},
    },
    find_missing_collaborators: {
        label: "Suggest collaborators connected to two known people.",
        params: {person_ids: [1, 2], limit: 5, min_combined_weight: 2},
    },
    find_people: {
        label: "Find people by name or email fragment.",
        params: {query: "Ada", limit: 5},
    },
    fts_search: {
        label: "Keyword-search the archive for meeting-note messages.",
        params: {query: "meeting notes", limit: 5},
    },
    fts_search_scoped: {
        label: "Search within one person's message history.",
        params: {person_id: 1, query: "meeting notes", limit: 5},
    },
    get_bundle_index: {
        label: "Load the compact index for a project bundle.",
        params: {kind: "project", project_id: 1},
    },
    get_cluster: {
        label: "Load social cluster data for one person.",
        params: {person_id: 1},
    },
    get_concept_bundle: {
        label: "Load a compact concept bundle.",
        params: {concept_id: 1, detail: "index"},
    },
    get_concept_summary: {
        label: "Load a brief concept summary.",
        params: {concept_id: 1, brief: true},
    },
    get_message: {
        label: "Load one source message.",
        params: {message_id: 607},
    },
    get_message_batch: {
        label: "Load a compact batch of source messages.",
        params: {message_ids: [607, 592, 599], include_body: false, body_char_limit: 1200, include_headers: true},
    },
    get_notes: {
        label: "Load notes for one person.",
        params: {person_id: 1},
    },
    get_person_network: {
        label: "Load a person's nearest social neighbors.",
        params: {nonce: "debug", person_id: 1, limit: 10},
    },
    get_person_summary: {
        label: "Load a brief person summary.",
        params: {person_id: 1},
    },
    get_project_bundle: {
        label: "Load a compact project bundle.",
        params: {project_id: 1, detail: "index"},
    },
    get_project_summary: {
        label: "Load a brief project summary.",
        params: {project_id: 1, brief: true},
    },
    get_thread: {
        label: "Load one conversation thread.",
        params: {thread_id: 514},
    },
    list_person_messages: {
        label: "List compact timeline messages for one person.",
        params: {person_id: 1, limit: 20, fields: "compact"},
    },
    search_concepts: {
        label: "Search concept rollups by text.",
        params: {query: "AI"},
    },
    search_persons: {
        label: "Search people rollups by text.",
        params: {query: "Ada"},
    },
    search_projects: {
        label: "Search project rollups by text.",
        params: {query: "travel"},
    },
    summarize_thread: {
        label: "Summarize representative snippets from one thread.",
        params: {thread_id: 514, max_messages: 8},
    },
    vector_search: {
        label: "Semantic-search the archive for meeting-note messages.",
        params: {query: "meeting notes", limit: 5},
    },
};

function parseParameters(parameters: JSONSchema | string): JSONSchema {
    if (typeof parameters === "string") {
        try {
            return JSON.parse(parameters) as JSONSchema;
        } catch {
            return {type: "object", properties: {}};
        }
    }
    return parameters ?? {type: "object", properties: {}};
}

function emptyValues(schema: JSONSchema): Record<string, FieldValue> {
    const out: Record<string, FieldValue> = {};
    for (const [key, prop] of Object.entries(schema.properties ?? {})) {
        out[key] = prop.type === "boolean" ? false : "";
    }
    return out;
}

function valuesFromParams(schema: JSONSchema, params: Record<string, unknown>): Record<string, FieldValue> {
    const out = emptyValues(schema);
    for (const [key, prop] of Object.entries(schema.properties ?? {})) {
        const value = params[key];
        if (typeof value === "undefined" || value === null) continue;
        if (prop.type === "boolean") {
            out[key] = Boolean(value);
        } else if (Array.isArray(value)) {
            if (prop.items?.type === "number" || prop.items?.type === "integer") {
                out[key] = value.join(", ");
            } else {
                out[key] = JSON.stringify(value, null, 2);
            }
        } else if (typeof value === "object") {
            out[key] = JSON.stringify(value, null, 2);
        } else {
            out[key] = String(value);
        }
    }
    return out;
}

function buildParams(schema: JSONSchema, values: Record<string, FieldValue>) {
    const params: Record<string, unknown> = {};
    for (const [key, prop] of Object.entries(schema.properties ?? {})) {
        const value = values[key];
        const required = (schema.required ?? []).includes(key);
        if (prop.type === "boolean") {
            params[key] = Boolean(value);
            continue;
        }
        if (typeof value !== "string") continue;
        const trimmed = value.trim();
        if (!required && trimmed === "") continue;
        if (prop.type === "number" || prop.type === "integer") {
            if (trimmed !== "") params[key] = Number(trimmed);
            continue;
        }
        if (prop.type === "array") {
            if (prop.items?.type === "number" || prop.items?.type === "integer") {
                params[key] = trimmed
                    .split(",")
                    .map((part) => part.trim())
                    .filter(Boolean)
                    .map(Number)
                    .filter((n) => Number.isFinite(n));
            } else {
                params[key] = trimmed ? JSON.parse(trimmed) : [];
            }
            continue;
        }
        if (prop.type === "object") {
            params[key] = trimmed ? JSON.parse(trimmed) : {};
            continue;
        }
        params[key] = trimmed;
    }
    return params;
}

function formatDuration(ms: number) {
    if (ms >= 60000) return `${(ms / 60000).toFixed(1)}m`;
    if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`;
    return `${ms}ms`;
}

function formatBytes(bytes: number) {
    if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${bytes} bytes`;
}

function formatTokens(n: number) {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M tok`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K tok`;
    return `${n} tok`;
}

function durationBadge(ms: number) {
    if (ms > 30000) return "border-red-200 bg-red-50 text-red-700";
    if (ms > 5000) return "border-amber-200 bg-amber-50 text-amber-700";
    return "border-blue-200 bg-blue-50 text-blue-700";
}

function statusBadge(status: number) {
    if (status >= 200 && status < 300) return "border-emerald-200 bg-emerald-50 text-emerald-700";
    return "border-red-200 bg-red-50 text-red-700";
}

function prettyJSON(value: unknown) {
    return JSON.stringify(value, null, 2);
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

function collectMessageArrays(value: unknown, out: MessageRecord[][] = []): MessageRecord[][] {
    if (Array.isArray(value)) {
        if (looksLikeMessages(value)) {
            out.push(value);
            return out;
        }
        for (const item of value) collectMessageArrays(item, out);
        return out;
    }
    if (value && typeof value === "object") {
        for (const item of Object.values(value as Record<string, unknown>)) {
            collectMessageArrays(item, out);
        }
    }
    return out;
}

function MetricPill({
                        icon,
                        label,
                        className,
                    }: {
    icon: ReactNode;
    label: string;
    className: string;
}) {
    return (
        <span
            className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-1 text-xs font-semibold ${className}`}>
      {icon}
            {label}
    </span>
    );
}

function MessageCard({msg}: { msg: MessageRecord }) {
    const [expanded, setExpanded] = useState(false);
    const body = msg.body_text ? stripHtml(msg.body_text) : null;
    const senderName = msg.sender_canonical_name ?? msg.from_name ?? msg.sender_primary_email ?? msg.from_email;
    const senderEmail = msg.sender_primary_email ?? msg.from_email;
    const dateStr = msg.date
        ? new Date(msg.date).toLocaleString([], {dateStyle: "medium", timeStyle: "short"})
        : null;

    return (
        <div className="overflow-hidden rounded-lg border border-outline-variant bg-white">
            <div className="space-y-1 px-4 py-3">
                <div className="flex items-start gap-2">
                    <p className="min-w-0 flex-1 text-sm font-semibold leading-snug text-on-surface">
                        {msg.subject ?? "(no subject)"}
                    </p>
                    {typeof msg.message_id !== "undefined" && (
                        <span
                            className="shrink-0 rounded border border-outline-variant bg-surface-container-low px-1.5 py-0.5 font-mono text-[10px] text-on-surface-variant">
              {msg.message_id}
            </span>
                    )}
                </div>
                <div className="flex items-center gap-1.5 text-xs text-on-surface-variant">
                    <Mail className="h-3 w-3 shrink-0"/>
                    <span className="truncate font-medium">{senderName}</span>
                    {senderEmail && senderName !== senderEmail && (
                        <span className="truncate text-on-surface-variant/60">{senderEmail}</span>
                    )}
                    {dateStr && <span className="ml-auto shrink-0 text-on-surface-variant/60">{dateStr}</span>}
                </div>
                {msg.snippet && (
                    <p className="line-clamp-2 text-xs italic leading-relaxed text-on-surface-variant">
                        {msg.snippet}
                    </p>
                )}
            </div>
            {body && (
                <>
                    <div className="flex items-center justify-between border-t border-outline-variant/50 px-4 py-2">
            <span className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant">
              Body
            </span>
                        <button
                            onClick={() => setExpanded((v) => !v)}
                            className="text-[10px] font-semibold text-primary hover:underline"
                        >
                            {expanded ? "Collapse" : "Expand"}
                        </button>
                    </div>
                    {expanded && (
                        <div className="px-4 pb-4">
              <pre
                  className="max-h-72 overflow-y-auto rounded border border-outline-variant bg-surface-container-low p-3 font-sans text-[11px] leading-relaxed text-on-surface whitespace-pre-wrap">
                {body}
              </pre>
                        </div>
                    )}
                </>
            )}
        </div>
    );
}

function PreBox({content}: { content: string }) {
    const [wrapped, setWrapped] = useState(false);
    return (
        <div className="min-w-0">
            <div className="mb-1.5 flex items-center justify-between">
                <div
                    className="flex items-center gap-1.5 font-mono text-[9px] font-bold uppercase tracking-wider text-on-surface-variant">
                    <Terminal className="h-3 w-3"/>
                    Raw JSON
                </div>
                <button
                    onClick={() => setWrapped((w) => !w)}
                    className={`rounded border px-1.5 py-0.5 text-[9px] font-semibold transition ${
                        wrapped
                            ? "border-outline-variant bg-primary-fixed text-on-primary-fixed-variant"
                            : "border-outline-variant bg-surface-container text-on-surface-variant hover:bg-surface-container-high"
                    }`}
                >
                    {wrapped ? "Unwrap" : "Wrap"}
                </button>
            </div>
            <pre
                className={`max-h-[520px] max-w-full rounded-lg border border-outline-variant bg-surface-container-low p-3 font-mono text-[10px] leading-relaxed text-on-surface ${
                    wrapped ? "overflow-y-auto whitespace-pre-wrap break-all" : "overflow-auto whitespace-pre"
                }`}
            >
        {content}
      </pre>
        </div>
    );
}

export default function DebugToolsPage() {
    const [manifest, setManifest] = useState<Record<string, ToolSchema>>({});
    const [selectedTool, setSelectedTool] = useState("");
    const [values, setValues] = useState<Record<string, FieldValue>>({});
    const [loadingManifest, setLoadingManifest] = useState(true);
    const [loadingInvoke, setLoadingInvoke] = useState(false);
    const [error, setError] = useState("");
    const [response, setResponse] = useState<InvokeEnvelope | null>(null);
    const [recent, setRecent] = useState<RecentInvocation[]>([]);

    useEffect(() => {
        let cancelled = false;

        async function loadManifest() {
            setLoadingManifest(true);
            setError("");
            try {
                const r = await fetch("/api/debug/tools/manifest", {cache: "no-store"});
                const data = await r.json();
                if (!r.ok) throw new Error(data?.error ?? `manifest failed: ${r.status}`);
                if (cancelled) return;
                const tools = data as Record<string, ToolSchema>;
                setManifest(tools);
                const search = new URLSearchParams(window.location.search);
                const urlTool = search.get("tool") ?? "";
                let initialTool = Object.keys(tools).sort()[0] ?? "";
                let prefill: Record<string, unknown> | null = null;
                if (urlTool && tools[urlTool]) {
                    initialTool = urlTool;
                    const rawArgs = search.get("args");
                    if (rawArgs) {
                        try {
                            const parsed = JSON.parse(rawArgs) as unknown;
                            if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
                                prefill = parsed as Record<string, unknown>;
                            }
                        } catch {
                            // Ignore malformed args; fall back to an empty form.
                        }
                    }
                }
                setSelectedTool(initialTool);
                if (initialTool) {
                    const schema = parseParameters(tools[initialTool].parameters);
                    setValues(prefill ? valuesFromParams(schema, prefill) : emptyValues(schema));
                }
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            } finally {
                if (!cancelled) setLoadingManifest(false);
            }
        }

        loadManifest();
        return () => {
            cancelled = true;
        };
    }, []);

    const selectedSchema = selectedTool ? manifest[selectedTool] : undefined;
    const parameters = useMemo(
        () => (selectedSchema ? parseParameters(selectedSchema.parameters) : {type: "object", properties: {}}),
        [selectedSchema],
    );
    const toolNames = useMemo(() => Object.keys(manifest).sort(), [manifest]);
    const prettyResponse = response ? prettyJSON(response) : "";
    const messageArrays = response ? collectMessageArrays(response.data) : [];
    const selectedSample = selectedTool ? TOOL_SAMPLES[selectedTool] : undefined;

    function selectTool(tool: string) {
        setSelectedTool(tool);
        setResponse(null);
        setError("");
        setValues(emptyValues(parseParameters(manifest[tool].parameters)));
    }

    function loadSample() {
        if (!selectedSample) return;
        setValues(valuesFromParams(parameters, selectedSample.params));
        setResponse(null);
        setError("");
    }

    async function invoke(paramsOverride?: Record<string, unknown>, toolOverride?: string) {
        const tool = toolOverride ?? selectedTool;
        const schema = manifest[tool];
        if (!tool || !schema) return;
        setLoadingInvoke(true);
        setError("");
        try {
            const params = paramsOverride ?? buildParams(parseParameters(schema.parameters), values);
            const r = await fetch("/api/debug/tools/invoke", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({tool, params}),
            });
            const envelope = (await r.json()) as InvokeEnvelope;
            setResponse(envelope);
            setRecent((items) => [
                {
                    id: Date.now(),
                    tool,
                    params,
                    status: envelope.status,
                    duration_ms: envelope.duration_ms,
                    estimated_tokens: envelope.estimated_tokens,
                },
                ...items,
            ].slice(0, 10));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setLoadingInvoke(false);
        }
    }

    return (
        <main className="min-h-screen overflow-x-hidden bg-surface-dim px-5 pb-6 pt-24">
            <div className="mx-auto max-w-7xl space-y-5">
                <div className="flex items-center justify-between gap-4">
                    <div>
                        <Link href="/debug"
                              className="mb-2 inline-flex items-center gap-1 text-sm font-medium text-primary hover:underline">
                            <ArrowLeft className="h-4 w-4"/>
                            Debug
                        </Link>
                        <h1 className="text-2xl font-bold text-on-surface">Tool Debugger</h1>
                        <p className="mt-1 max-w-3xl text-sm text-on-surface-variant">
                            Invoke live read-only Go-backed tools and inspect payload size, timing, and raw JSON.
                            Bound-agent tools require manual IDs here because no real run context is present.
                        </p>
                    </div>
                    <div
                        className="hidden items-center gap-2 rounded-lg border border-outline-variant bg-white px-3 py-2 text-xs text-on-surface-variant sm:flex">
                        <Database className="h-4 w-4"/>
                        Read-only tools only
                    </div>
                </div>

                {error && (
                    <div
                        className="flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0"/>
                        <span>{error}</span>
                    </div>
                )}

                <div className="grid gap-5 lg:grid-cols-[minmax(320px,420px)_minmax(0,1fr)]">
                    <section className="min-w-0 rounded-lg border border-outline-variant bg-white p-4">
                        <div className="mb-4 flex items-center gap-2">
                            <Activity className="h-4 w-4 text-primary"/>
                            <h2 className="text-sm font-bold text-on-surface">Invoke Tool</h2>
                        </div>

                        <label className="block text-xs font-semibold text-on-surface-variant" htmlFor="tool-select">
                            Tool
                        </label>
                        <select
                            id="tool-select"
                            value={selectedTool}
                            onChange={(event) => selectTool(event.target.value)}
                            disabled={loadingManifest}
                            className="mt-1 w-full rounded-md border border-outline-variant bg-white px-3 py-2 text-sm text-on-surface outline-none focus:border-primary"
                        >
                            {loadingManifest ? (
                                <option>Loading tools...</option>
                            ) : (
                                toolNames.map((tool) => <option key={tool} value={tool}>{tool}</option>)
                            )}
                        </select>

                        {selectedSchema && (
                            <p className="mt-2 text-xs leading-relaxed text-on-surface-variant">
                                {selectedSchema.description}
                            </p>
                        )}

                        {selectedSample && (
                            <div className="mt-3 rounded-md border border-outline-variant bg-surface-container-low p-3">
                                <div className="flex items-start justify-between gap-3">
                                    <div className="min-w-0">
                                        <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant">
                                            Sample
                                        </p>
                                        <p className="mt-1 text-xs leading-relaxed text-on-surface-variant">
                                            {selectedSample.label}
                                        </p>
                                    </div>
                                    <button
                                        type="button"
                                        onClick={loadSample}
                                        className="inline-flex shrink-0 items-center gap-1 rounded border border-outline-variant bg-white px-2 py-1 text-xs font-semibold text-on-surface-variant hover:bg-surface-container"
                                    >
                                        <WandSparkles className="h-3 w-3"/>
                                        Load
                                    </button>
                                </div>
                                <pre
                                    className="mt-2 max-h-28 overflow-auto rounded border border-outline-variant/70 bg-white p-2 font-mono text-[10px] leading-relaxed text-on-surface">
                  {prettyJSON(selectedSample.params)}
                </pre>
                            </div>
                        )}

                        <div className="mt-5 space-y-4">
                            {Object.entries(parameters.properties ?? {}).length === 0 && (
                                <p className="rounded-md border border-outline-variant bg-surface-container-low px-3 py-2 text-xs text-on-surface-variant">
                                    This tool has no parameters.
                                </p>
                            )}
                            {Object.entries(parameters.properties ?? {}).map(([name, prop]) => {
                                const required = (parameters.required ?? []).includes(name);
                                const id = `field-${name}`;
                                const description = prop.description ?? "";
                                return (
                                    <div key={name}>
                                        <label htmlFor={id} className="text-xs font-semibold text-on-surface">
                                            {name}
                                            {required && <span className="text-red-600"> *</span>}
                                        </label>
                                        <div className="mt-1">
                                            {prop.enum ? (
                                                <select
                                                    id={id}
                                                    value={String(values[name] ?? "")}
                                                    onChange={(event) => setValues((v) => ({
                                                        ...v,
                                                        [name]: event.target.value
                                                    }))}
                                                    className="w-full rounded-md border border-outline-variant bg-white px-3 py-2 text-sm text-on-surface outline-none focus:border-primary"
                                                >
                                                    <option value="">Select...</option>
                                                    {prop.enum.map((option) => <option key={option}
                                                                                       value={option}>{option}</option>)}
                                                </select>
                                            ) : prop.type === "boolean" ? (
                                                <input
                                                    id={id}
                                                    type="checkbox"
                                                    checked={Boolean(values[name])}
                                                    onChange={(event) => setValues((v) => ({
                                                        ...v,
                                                        [name]: event.target.checked
                                                    }))}
                                                    className="h-4 w-4 rounded border-outline-variant text-primary"
                                                />
                                            ) : prop.type === "array" && prop.items?.type !== "number" && prop.items?.type !== "integer" ? (
                                                <textarea
                                                    id={id}
                                                    value={String(values[name] ?? "")}
                                                    onChange={(event) => setValues((v) => ({
                                                        ...v,
                                                        [name]: event.target.value
                                                    }))}
                                                    placeholder="JSON array"
                                                    rows={5}
                                                    className="w-full rounded-md border border-outline-variant bg-white px-3 py-2 font-mono text-xs text-on-surface outline-none focus:border-primary"
                                                />
                                            ) : prop.type === "object" ? (
                                                <textarea
                                                    id={id}
                                                    value={String(values[name] ?? "")}
                                                    onChange={(event) => setValues((v) => ({
                                                        ...v,
                                                        [name]: event.target.value
                                                    }))}
                                                    placeholder="JSON object"
                                                    rows={5}
                                                    className="w-full rounded-md border border-outline-variant bg-white px-3 py-2 font-mono text-xs text-on-surface outline-none focus:border-primary"
                                                />
                                            ) : (
                                                <input
                                                    id={id}
                                                    type={prop.type === "number" || prop.type === "integer" ? "number" : "text"}
                                                    value={String(values[name] ?? "")}
                                                    onChange={(event) => setValues((v) => ({
                                                        ...v,
                                                        [name]: event.target.value
                                                    }))}
                                                    placeholder={prop.type === "array" ? "Comma-separated numbers" : description}
                                                    className="w-full rounded-md border border-outline-variant bg-white px-3 py-2 text-sm text-on-surface outline-none focus:border-primary"
                                                />
                                            )}
                                        </div>
                                        {description && (
                                            <p className="mt-1 text-xs italic leading-relaxed text-on-surface-variant">
                                                {description}
                                            </p>
                                        )}
                                    </div>
                                );
                            })}
                        </div>

                        <button
                            onClick={() => invoke()}
                            disabled={!selectedTool || loadingInvoke || loadingManifest}
                            className="mt-5 inline-flex w-full items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-semibold text-on-primary transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
                        >
                            {loadingInvoke ? (
                                <>
                                    <RotateCcw className="h-4 w-4 animate-spin"/>
                                    Invoking
                                </>
                            ) : (
                                <>
                                    <Play className="h-4 w-4"/>
                                    Invoke
                                </>
                            )}
                        </button>
                    </section>

                    <section className="min-w-0 space-y-5">
                        <div className="min-w-0 rounded-lg border border-outline-variant bg-white p-4">
                            <div className="mb-4 flex items-center justify-between gap-3">
                                <div className="flex items-center gap-2">
                                    <Terminal className="h-4 w-4 text-primary"/>
                                    <h2 className="text-sm font-bold text-on-surface">Response</h2>
                                </div>
                                {response && (
                                    <div className="flex flex-wrap justify-end gap-2">
                                        <MetricPill icon={<CheckCircle className="h-3 w-3"/>}
                                                    label={String(response.status)}
                                                    className={statusBadge(response.status)}/>
                                        <MetricPill icon={<Clock className="h-3 w-3"/>}
                                                    label={formatDuration(response.duration_ms)}
                                                    className={durationBadge(response.duration_ms)}/>
                                        <MetricPill icon={<Database className="h-3 w-3"/>}
                                                    label={formatBytes(response.response_size_bytes)}
                                                    className="border-outline-variant bg-surface-container-low text-on-surface-variant"/>
                                        <MetricPill icon={<Activity className="h-3 w-3"/>}
                                                    label={formatTokens(response.estimated_tokens)}
                                                    className="border-outline-variant bg-surface-container-low text-on-surface-variant"/>
                                    </div>
                                )}
                            </div>
                            {response ? (
                                <div className="min-w-0 space-y-4">
                                    <PreBox content={prettyResponse}/>
                                    {messageArrays.length > 0 && (
                                        <div className="min-w-0">
                                            <div
                                                className="mb-2 flex items-center gap-1.5 text-[9px] font-bold uppercase tracking-wider text-on-surface-variant">
                                                <Mail className="h-3 w-3"/>
                                                Message Results
                                            </div>
                                            <div className="space-y-3">
                                                {messageArrays.flatMap((arr, groupIndex) =>
                                                    arr.map((msg, index) => (
                                                        <MessageCard key={`${groupIndex}-${msg.message_id ?? index}`}
                                                                     msg={msg}/>
                                                    )),
                                                )}
                                            </div>
                                        </div>
                                    )}
                                </div>
                            ) : (
                                <div
                                    className="rounded-lg border border-dashed border-outline-variant bg-surface-container-low px-4 py-12 text-center text-sm text-on-surface-variant">
                                    Select a tool, enter parameters, and invoke it to inspect the live response.
                                </div>
                            )}
                        </div>

                        {recent.length > 0 && (
                            <div className="min-w-0 rounded-lg border border-outline-variant bg-white p-4">
                                <h2 className="mb-3 text-sm font-bold text-on-surface">Recent Invocations</h2>
                                <div className="space-y-2">
                                    {recent.map((item) => (
                                        <div key={item.id}
                                             className="flex items-center gap-3 rounded-md border border-outline-variant bg-surface-container-low px-3 py-2">
                                            <div className="min-w-0 flex-1">
                                                <p className="truncate font-mono text-xs font-semibold text-on-surface">{item.tool}</p>
                                                <p className="text-xs text-on-surface-variant">
                                                    {item.status} · {formatDuration(item.duration_ms)} · {formatTokens(item.estimated_tokens)}
                                                </p>
                                            </div>
                                            <button
                                                onClick={() => invoke(item.params, item.tool)}
                                                disabled={loadingInvoke}
                                                className="inline-flex items-center gap-1 rounded border border-outline-variant bg-white px-2 py-1 text-xs font-semibold text-on-surface-variant hover:bg-surface-container"
                                            >
                                                <RotateCcw className="h-3 w-3"/>
                                                Re-run
                                            </button>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        )}
                    </section>
                </div>
            </div>
        </main>
    );
}
