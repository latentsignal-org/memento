"use client";

import {useEffect, useRef, useState} from "react";
import Link from "next/link";
import {useMessageDetail} from "@/components/evidence/useMessageDetail";
import MessagePreviewPanel from "@/components/evidence/MessagePreviewPanel";

// ---- Types (shape matches social.GroupDetail in the Go backend) -------------

export type GroupMember = {
    person_id: number;
    canonical_name: string;
    primary_email: string;
    slug: string;
    weighted_degree: number;
    excluded?: boolean;
    added_by_user?: boolean;
};

export type GroupThread = {
    thread_id: string;
    message_id: string;
    internal_msg_id: number;
    subject: string;
    from_name: string;
    from_email: string;
    internal_ts: number; // unix seconds
};

export type Group = {
    group_id: number;
    size: number;
    density: number;
    label: string;
    display_name: string;
    note: string;
    is_actionable: boolean;
    suppression_reason?: string;
    saved_at?: string;
    dismissed_at?: string;
    members: GroupMember[];
    top_threads: GroupThread[];
    cadence: number[];
    message_count: number;
    last_activity_ts: number; // unix seconds
};

// ---- Helpers ----------------------------------------------------------------

function relativeTime(epoch: number): string {
    if (!epoch) return "";
    const now = Math.floor(Date.now() / 1000);
    const d = now - epoch;
    if (d < 60) return "just now";
    if (d < 3600) return `${Math.floor(d / 60)} m`;
    if (d < 86400) return `${Math.floor(d / 3600)} h`;
    if (d < 86400 * 14) return `${Math.floor(d / 86400)} d`;
    if (d < 86400 * 60) return `${Math.floor(d / (86400 * 7))} w`;
    if (d < 86400 * 365) return `${Math.floor(d / (86400 * 30))} mo`;
    return `${Math.floor(d / (86400 * 365))} y`;
}

function displayTitle(g: Group): string {
    if (g.display_name) return g.display_name;
    if (g.label) return g.label;
    const named = g.members.filter((m) => !m.excluded && m.canonical_name).slice(0, 3);
    if (named.length === 0) return "Unlabeled group";
    return named.map((m) => m.canonical_name).join(", ");
}

function densityLabel(d: number): string {
    return d >= 0.05 ? "tightly connected" : "loosely connected";
}

// Activity summary from the persisted all-time stats. Falls back to the newest
// top-thread timestamp if last_activity_ts is missing (older cached rows).
function lastActivityLabel(g: Group): string {
    let ts = g.last_activity_ts || 0;
    if (!ts && g.top_threads && g.top_threads.length > 0) {
        ts = g.top_threads[0].internal_ts || 0;
    }
    if (!ts) return "no dated activity";
    const monthYear = new Intl.DateTimeFormat("en-US", {
        month: "short",
        year: "numeric",
    }).format(new Date(ts * 1000));
    return `last activity ${relativeTime(ts)} ago (${monthYear})`;
}

function lastActivityParts(g: Group): { prefix: string; value: string } {
    const label = lastActivityLabel(g);
    const prefix = "last activity ";
    if (!label.startsWith(prefix)) {
        return {prefix: "", value: label};
    }
    return {prefix, value: label.slice(prefix.length)};
}

function activityValues(g: Group): number[] {
    const cadence = g.cadence?.length === 12 ? g.cadence : [];
    if (cadence.some((v) => v > 0)) return cadence;

    const timestamps = (g.top_threads ?? [])
        .map((t) => t.internal_ts || 0)
        .filter((ts) => ts > 0)
        .sort((a, b) => a - b);
    const out = new Array(12).fill(0);
    if (timestamps.length === 0) return out;
    const min = timestamps[0];
    const max = timestamps[timestamps.length - 1];
    if (min === max) {
        out[11] = timestamps.length;
        return out;
    }
    const span = max - min + 1;
    timestamps.forEach((ts) => {
        const idx = Math.min(11, Math.max(0, Math.floor(((ts - min) * 12) / span)));
        out[idx] += 1;
    });
    return out;
}

// ---- Sparkline --------------------------------------------------------------

function Sparkline({values}: { values: number[] }) {
    const max = Math.max(1, ...values);
    const CHART_H = 34;
    const MIN_BAR = 3;
    const MIN_ACTIVE = 8;
    return (
        <span
            className="inline-flex h-10 w-[156px] shrink-0 items-end justify-center gap-1 rounded bg-primary/5 px-2 py-1"
            aria-label="Group activity over time"
        >
      {values.map((v, i) => {
          let h: number;
          if (v === 0) {
              h = MIN_BAR;
          } else {
              h = Math.max(MIN_ACTIVE, Math.round((v / max) * CHART_H));
          }
          return (
              <span
                  key={i}
                  className={`block w-[6px] rounded-[1px] ${v === 0 ? "bg-primary/12" : "bg-primary opacity-90"}`}
                  style={{height: `${h}px`}}
              />
          );
      })}
    </span>
    );
}

// ---- Inline title edit ------------------------------------------------------

function TitleEditor({
                         value,
                         onSave,
                         onCancel,
                         onRegenerate,
                         regenerating,
                     }: {
    value: string;
    onSave: (next: string) => void;
    onCancel: () => void;
    onRegenerate: () => Promise<string | null>;
    regenerating: boolean;
}) {
    const [draft, setDraft] = useState(value);
    const inputRef = useRef<HTMLInputElement>(null);
    const containerRef = useRef<HTMLDivElement>(null);
    useEffect(() => {
        const raf = requestAnimationFrame(() => {
            inputRef.current?.focus();
            inputRef.current?.select();
        });
        return () => cancelAnimationFrame(raf);
    }, []);

    async function handleRegenerate() {
        const next = await onRegenerate();
        if (!next) return;
        setDraft(next);
        requestAnimationFrame(() => {
            inputRef.current?.focus();
            inputRef.current?.setSelectionRange(next.length, next.length);
        });
    }

    return (
        <div
            ref={containerRef}
            className="flex min-h-9 flex-1 min-w-0 items-center gap-1 rounded-lg border border-primary bg-white pl-3 pr-1 shadow-[0_0_0_3px_rgba(18,61,52,0.08)]"
            title="Press Enter to save, Esc to cancel"
            onBlur={(e) => {
                if (!containerRef.current?.contains(e.relatedTarget as Node)) {
                    onCancel();
                }
            }}
        >
            <input
                ref={inputRef}
                type="text"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === "Enter") onSave(draft.trim());
                    if (e.key === "Escape") onCancel();
                }}
                className="min-w-0 flex-1 border-none bg-transparent py-1 text-[13px] font-semibold leading-5 text-on-surface outline-none"
            />
            <button
                type="button"
                onClick={handleRegenerate}
                disabled={regenerating}
                title="Regenerate label with AI"
                className="inline-flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-md text-primary hover:bg-primary-container/40 disabled:opacity-50"
            >
        <span className={`material-symbols-outlined text-base ${regenerating ? "animate-spin" : ""}`}>
          {regenerating ? "sync" : "auto_awesome"}
        </span>
            </button>
            <button
                type="button"
                onClick={() => onSave(draft.trim())}
                title="Save (Enter)"
                className="inline-flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-md bg-primary text-white hover:bg-primary/90"
            >
                <span className="material-symbols-outlined text-base">check</span>
            </button>
        </div>
    );
}

// ---- Member chip ------------------------------------------------------------

function MemberChip({
                        member,
                        editable,
                        onToggleExclude,
                    }: {
    member: GroupMember;
    editable: boolean;
    onToggleExclude?: (m: GroupMember) => void;
}) {
    const chipClass = `group/chip inline-flex min-h-[22px] items-center gap-1.5 rounded border border-outline-variant/35 bg-surface-container-high pl-2.5 ${editable ? "pr-1" : "pr-2.5"} py-0.5 text-[10px] font-mono font-bold leading-none text-on-surface-variant transition-colors ${
        member.excluded
            ? "opacity-60 line-through"
            : "hover:border-outline-variant/60 hover:bg-surface-container-highest hover:text-on-surface"
    }`;

    const content = (
        <>
            {member.canonical_name || "(unnamed)"}
            {member.added_by_user && !member.excluded && (
                <span className="material-symbols-outlined text-[12px] leading-none text-primary/70"
                      title="Added by you">
          person_add
        </span>
            )}
            {editable && onToggleExclude && (
                <button
                    type="button"
                    onClick={(e) => {
                        e.preventDefault();
                        onToggleExclude(member);
                    }}
                    title={member.excluded ? "Restore to group" : "Remove from group"}
                    aria-label={member.excluded ? "Restore member" : "Remove member from group"}
                    className="inline-flex h-2.5 w-2.5 flex-shrink-0 items-center justify-center rounded text-on-surface-variant/40 opacity-0 transition-opacity hover:bg-error-container hover:text-error group-hover/chip:opacity-100 cursor-pointer"
                >
          <span className="material-symbols-outlined text-[8px] leading-none">
            {member.excluded ? "restore" : "close"}
          </span>
                </button>
            )}
        </>
    );

    if (member.slug && !member.excluded) {
        return (
            <Link href={`/people/${member.slug}`} className={chipClass} title={member.primary_email || undefined}>
                {content}
            </Link>
        );
    }
    return <span className={chipClass} title={member.primary_email || undefined}>{content}</span>;
}

// ---- Note editor ------------------------------------------------------------

function NoteBlock({
                       value,
                       onSave,
                   }: {
    value: string;
    onSave: (next: string) => void;
}) {
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState(value);
    useEffect(() => setDraft(value), [value]);

    if (editing) {
        return (
            <textarea
                autoFocus
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onBlur={() => {
                    setEditing(false);
                    if (draft !== value) onSave(draft);
                }}
                rows={3}
                placeholder="Add a note about this group…"
                className="w-full rounded-lg border border-outline-variant/50 bg-white px-3.5 py-2.5 text-[13px] leading-6 text-on-surface focus:outline-none focus:border-primary"
            />
        );
    }
    if (!value) {
        return (
            <button
                type="button"
                onClick={() => setEditing(true)}
                className="w-full text-left text-xs text-on-surface-variant/70 px-3 py-2 rounded-lg border border-outline-variant/50 bg-white hover:border-outline-variant cursor-pointer flex items-center gap-1.5"
            >
                <span className="material-symbols-outlined text-[13px] text-on-surface-variant/50">edit_note</span>
                Add a note about this group…
            </button>
        );
    }
    return (
        <button
            type="button"
            onClick={() => setEditing(true)}
            className="group/note w-full rounded-lg border border-outline-variant/40 bg-white px-3.5 py-3 text-left hover:border-outline-variant/70 cursor-pointer transition-colors"
            title="Click to edit note"
        >
      <span className="flex items-start gap-3">
        <span className="mt-0.5 h-5 w-1 shrink-0 rounded-full bg-primary/35" aria-hidden="true"/>
        <span className="min-w-0 flex-1 whitespace-pre-wrap text-[13px] font-medium leading-6 text-on-surface">
          {value}
        </span>
        <span
            className="material-symbols-outlined text-[13px] text-on-surface-variant/35 opacity-0 group-hover/note:opacity-100 transition-opacity">
          edit
        </span>
      </span>
        </button>
    );
}

// ---- Add-member control -----------------------------------------------------

function AddMember({
                       onAdd,
                       busy,
                   }: {
    onAdd: (personId: number) => Promise<void>;
    busy: boolean;
}) {
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const [results, setResults] = useState<Array<{ id: number; canonical_name: string; primary_email: string }>>([]);
    const [searching, setSearching] = useState(false);

    useEffect(() => {
        if (!open || query.trim().length < 2) {
            setResults([]);
            return;
        }
        const ctrl = new AbortController();
        setSearching(true);
        const t = setTimeout(async () => {
            try {
                const res = await fetch(`/api/people?q=${encodeURIComponent(query)}&top=8`, {
                    signal: ctrl.signal,
                });
                if (!res.ok) return;
                const data = await res.json();
                setResults(
                    (data.people || []).map((p: any) => ({
                        id: p.person_id,
                        canonical_name: p.canonical_name || "(unnamed)",
                        primary_email: p.primary_email || "",
                    })),
                );
            } finally {
                setSearching(false);
            }
        }, 200);
        return () => {
            ctrl.abort();
            clearTimeout(t);
        };
    }, [open, query]);

    if (!open) {
        return (
            <button
                type="button"
                onClick={() => setOpen(true)}
                disabled={busy}
                className="inline-flex min-h-[22px] items-center gap-1 rounded border border-dashed border-outline-variant/60 px-2.5 py-0.5 text-[10px] font-mono font-bold text-on-surface-variant hover:border-outline-variant hover:text-on-surface cursor-pointer disabled:opacity-50"
                title="Add a person to this group"
            >
                <span className="material-symbols-outlined text-[11px] leading-none">add</span>
                Add member
            </button>
        );
    }
    return (
        <div className="relative inline-flex">
            <input
                autoFocus
                type="text"
                value={query}
                placeholder="Search people…"
                onChange={(e) => setQuery(e.target.value)}
                onBlur={() => setTimeout(() => setOpen(false), 150)}
                className="px-3 py-1 rounded border border-primary text-xs bg-white outline-none w-48"
            />
            {(results.length > 0 || searching) && (
                <div
                    className="absolute top-full left-0 mt-1 w-64 max-h-56 overflow-y-auto bg-white border border-outline-variant rounded-lg shadow-lg z-10">
                    {searching && (
                        <div className="px-3 py-2 text-xs text-on-surface-variant">Searching…</div>
                    )}
                    {results.map((p) => (
                        <button
                            key={p.id}
                            type="button"
                            onMouseDown={async (e) => {
                                e.preventDefault();
                                await onAdd(p.id);
                                setOpen(false);
                                setQuery("");
                            }}
                            className="block w-full text-left px-3 py-2 text-xs hover:bg-surface-container cursor-pointer"
                        >
                            <div className="font-semibold text-on-surface">{p.canonical_name}</div>
                            <div className="text-on-surface-variant truncate">{p.primary_email}</div>
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}

// ---- GroupCard --------------------------------------------------------------

export interface GroupCardProps {
    group: Group;
    onChange: (next: Group) => void;
    onRemove: (groupId: number) => void; // called after dismiss to drop from list
}

export default function GroupCard({group, onChange, onRemove}: GroupCardProps) {
    const saved = !!group.saved_at;
    const [busy, setBusy] = useState<string | null>(null);
    const [editingTitle, setEditingTitle] = useState(false);
    const [showAllThreads, setShowAllThreads] = useState(false);
    const [previewMessageId, setPreviewMessageId] = useState<number | null>(null);
    const {detail: previewDetail, isLoading: previewLoading, error: previewError} = useMessageDetail(previewMessageId);

    const title = displayTitle(group);
    const members = group.members ?? [];
    const topThreads = group.top_threads ?? [];
    const chartValues = activityValues(group);
    const activity = lastActivityParts(group);
    const visibleTopThreads = showAllThreads ? topThreads : topThreads.slice(0, 4);
    const activeMembers = members.filter((m) => !m.excluded);
    const memberCount = activeMembers.length || group.size;

    // ---- Server interactions ----
    async function callAPI(path: string, init: RequestInit = {}) {
        setBusy(path);
        try {
            const res = await fetch(path, {cache: "no-store", ...init});
            if (!res.ok && res.status !== 204) {
                const text = await res.text();
                throw new Error(text || `${init.method ?? "GET"} ${path} → ${res.status}`);
            }
            return res;
        } finally {
            setBusy(null);
        }
    }

    async function refetchGroup() {
        const res = await fetch(`/api/social/groups/${group.group_id}`, {cache: "no-store"});
        if (!res.ok) return;
        const data = await res.json();
        const fresh = data.group as Group | undefined;
        if (fresh) onChange(fresh);
    }

    async function handleSave() {
        try {
            await callAPI(`/api/social/groups/${group.group_id}/save`, {method: "POST"});
            await refetchGroup();
        } catch (e) {
            alert(`Save failed: ${e instanceof Error ? e.message : e}`);
        }
    }

    async function handleDismiss() {
        if (!confirm(`Exclude "${title}" from the Groups view?`)) return;
        try {
            await callAPI(`/api/social/groups/${group.group_id}/dismiss`, {method: "POST"});
            onRemove(group.group_id);
        } catch (e) {
            alert(`Dismiss failed: ${e instanceof Error ? e.message : e}`);
        }
    }

    async function handleRename(next: string) {
        setEditingTitle(false);
        if (!next || next === title) return;
        try {
            await callAPI(`/api/social/groups/${group.group_id}`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({display_name: next}),
            });
            onChange({...group, display_name: next});
        } catch (e) {
            alert(`Rename failed: ${e instanceof Error ? e.message : e}`);
        }
    }

    async function handleRegenerateLabel() {
        try {
            const res = await callAPI(`/api/social/groups/${group.group_id}/label`, {method: "POST"});
            const data = await res.json();
            if (data.label) {
                return data.label as string;
            }
            return null;
        } catch (e) {
            alert(`Regenerate failed: ${e instanceof Error ? e.message : e}`);
            return null;
        }
    }

    async function handleNoteSave(next: string) {
        try {
            await callAPI(`/api/social/groups/${group.group_id}`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({note: next}),
            });
            onChange({...group, note: next});
        } catch (e) {
            alert(`Note save failed: ${e instanceof Error ? e.message : e}`);
        }
    }

    async function handleToggleMember(m: GroupMember) {
        try {
            const method = m.excluded ? "DELETE" : "POST";
            await callAPI(
                `/api/social/groups/${group.group_id}/members/${m.person_id}/exclude`,
                {method},
            );
            await refetchGroup();
        } catch (e) {
            alert(`Member update failed: ${e instanceof Error ? e.message : e}`);
        }
    }

    async function handleAddMember(personId: number) {
        try {
            await callAPI(`/api/social/groups/${group.group_id}/members`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({person_id: personId}),
            });
            await refetchGroup();
        } catch (e) {
            alert(`Add member failed: ${e instanceof Error ? e.message : e}`);
        }
    }

    // ---- Render ----
    return (
        <article
            className="relative flex flex-col p-5 rounded-2xl border border-outline-variant/40 bg-surface-container-low transition-all duration-200 hover:border-outline-variant"
        >
            {/* top-right: edit + dismiss */}
            <div className="absolute top-3 right-3 flex items-center gap-0.5">
                {!editingTitle ? (
                    <>
                        <button
                            type="button"
                            onClick={() => setEditingTitle(true)}
                            title="Rename"
                            aria-label="Rename group"
                            className="w-7 h-7 rounded-lg inline-flex items-center justify-center text-on-surface-variant/50 hover:bg-surface-container hover:text-on-surface cursor-pointer"
                        >
                            <span className="material-symbols-outlined text-base">edit</span>
                        </button>
                        <button
                            type="button"
                            onClick={handleDismiss}
                            disabled={!!busy}
                            title={saved ? "Archive this group" : "Exclude this group from suggestions"}
                            aria-label="Dismiss group"
                            className="w-7 h-7 rounded-lg inline-flex items-center justify-center text-on-surface-variant/50 hover:bg-error-container hover:text-error cursor-pointer disabled:opacity-50"
                        >
                            <span className="material-symbols-outlined text-base">close</span>
                        </button>
                    </>
                ) : null}
            </div>

            {/* title row */}
            <div className={`mb-1 ${editingTitle ? "" : "pr-16"}`}>
                {editingTitle ? (
                    <TitleEditor
                        value={title}
                        onSave={handleRename}
                        onCancel={() => setEditingTitle(false)}
                        onRegenerate={handleRegenerateLabel}
                        regenerating={busy === `/api/social/groups/${group.group_id}/label`}
                    />
                ) : (
                    <h3
                        className="text-headline-sm font-bold text-primary [overflow-wrap:anywhere]"
                    >
                        {title}
                    </h3>
                )}
            </div>

            <p className="text-[11px] text-on-surface-variant/80 mb-3">
                {memberCount} {memberCount === 1 ? "person" : "people"}
                {" · "}
                {densityLabel(group.density)}
                {" · "}
                <span className={saved ? "text-primary font-semibold" : "text-on-surface-variant"}>
          {saved ? "saved" : "candidate"}
        </span>
            </p>

            {/* Members */}
            <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant/70 mb-2">
                Members
            </p>
            <div className="flex flex-wrap gap-1.5 mb-4">
                {members.length === 0 && (
                    <span className="text-xs italic text-on-surface-variant/60">No members</span>
                )}
                {members.map((m) => (
                    <MemberChip
                        key={m.person_id}
                        member={m}
                        editable
                        onToggleExclude={handleToggleMember}
                    />
                ))}
                <AddMember onAdd={handleAddMember} busy={!!busy}/>
            </div>

            {/* Note + Generate brief (saved only) */}
            {saved && (
                <>
                    <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant/70 mb-2">
                        Note
                    </p>
                    <div className="mb-2">
                        <NoteBlock value={group.note} onSave={handleNoteSave}/>
                    </div>
                    <div className="flex justify-end mb-4">
                        <button
                            type="button"
                            disabled
                            title="Generate brief — coming soon"
                            className="inline-flex items-center gap-1.5 rounded-2xl border border-outline-variant/60 bg-background px-3 py-1.5 text-[11px] font-semibold text-on-surface-variant/70 cursor-not-allowed"
                        >
                            <span className="material-symbols-outlined text-[13px]">description</span>
                            Generate brief
                            <span
                                className="text-[9px] uppercase tracking-wide text-on-surface-variant/50 ml-0.5">soon</span>
                        </button>
                    </div>
                </>
            )}

            {/* Activity: all-time cadence chart + all-time message count & recency. */}
            <div className="mb-5 mt-1 flex items-center gap-3">
                <Sparkline values={chartValues}/>
                <span className="min-w-0 text-[11.5px] leading-5 text-on-surface-variant">
          <span className="text-on-surface font-semibold">{group.message_count}</span>
                    {group.message_count === 1 ? " message" : " messages"}
                    {" · "}
                    {activity.prefix}
                    <span className="text-on-surface font-semibold">{activity.value}</span>
        </span>
            </div>

            {/* Top recent threads */}
            <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant/70 mb-2 mt-1">
                Top recent threads
            </p>
            <div className="mb-4 border border-outline-variant/40 rounded-xl overflow-hidden bg-white">
                {topThreads.length === 0 ? (
                    <p className="text-xs italic text-on-surface-variant/60 px-3 py-3">
                        No recent threads found.
                    </p>
                ) : (
                    <>
                        <ul className="divide-y divide-outline-variant/30">
                            {visibleTopThreads.map((t) => (
                                <li
                                    key={t.thread_id}
                                    onClick={() => {
                                        if (t.internal_msg_id > 0) setPreviewMessageId(t.internal_msg_id);
                                    }}
                                    title={t.from_email || undefined}
                                    className={`grid grid-cols-[72px_minmax(0,1fr)_auto] gap-1.5 items-baseline px-3 py-2 transition-colors ${
                                        t.internal_msg_id > 0 ? "cursor-pointer hover:bg-surface-container/40" : ""
                                    }`}
                                >
                  <span className="text-[11px] font-semibold text-on-surface-variant truncate">
                    {t.from_name || t.from_email || "—"}
                  </span>
                                    <span className="text-[12px] text-on-surface truncate">
                    {t.subject || "(no subject)"}
                  </span>
                                    <span className="text-[10.5px] text-on-surface-variant tabular-nums text-right">
                    {relativeTime(t.internal_ts)}
                  </span>
                                </li>
                            ))}
                        </ul>
                        {topThreads.length > 4 && (
                            <button
                                type="button"
                                onClick={() => setShowAllThreads((current) => !current)}
                                className="w-full flex items-center justify-center gap-1 px-3 py-2 bg-surface-container/40 border-t border-outline-variant/30 text-[11px] font-semibold text-primary hover:bg-primary-container/20 transition-colors cursor-pointer"
                            >
                                {showAllThreads ? "Show less" : "Show more"}
                                <span className="material-symbols-outlined text-sm">
                  {showAllThreads ? "expand_less" : "expand_more"}
                </span>
                            </button>
                        )}
                    </>
                )}
            </div>

            {/* Actions — candidate only */}
            {!saved && (
                <div className="flex flex-wrap gap-2 mt-auto">
                    <button
                        type="button"
                        onClick={handleSave}
                        disabled={!!busy}
                        className="inline-flex items-center gap-1.5 rounded-2xl border border-primary/20 bg-primary-fixed/35 px-3.5 py-1.5 text-[11px] font-semibold text-primary shadow-sm transition-colors hover:bg-primary-fixed/55 disabled:opacity-50 cursor-pointer"
                    >
                        <span className="material-symbols-outlined text-xs">bookmark_add</span>
                        Save group
                    </button>
                </div>
            )}

            {/* Email preview modal */}
            {previewMessageId !== null && (
                <div
                    className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-6"
                    onClick={() => setPreviewMessageId(null)}
                >
                    <div
                        className="bg-background border border-outline-variant rounded-2xl shadow-xl max-w-2xl w-full max-h-[80vh] flex flex-col overflow-hidden"
                        onClick={(e) => e.stopPropagation()}
                    >
                        <div className="flex items-center justify-between px-5 py-3 border-b border-outline-variant/60">
                            <span className="text-sm font-semibold text-on-surface">Email preview</span>
                            <button
                                onClick={() => setPreviewMessageId(null)}
                                className="w-8 h-8 rounded-lg hover:bg-surface-container text-on-surface-variant inline-flex items-center justify-center cursor-pointer"
                                aria-label="Close"
                            >
                                <span className="material-symbols-outlined text-lg">close</span>
                            </button>
                        </div>
                        <div className="overflow-y-auto">
                            <MessagePreviewPanel
                                detail={previewDetail}
                                isLoading={previewLoading}
                                error={previewError}
                                emptyText="Loading message…"
                            />
                        </div>
                    </div>
                </div>
            )}
        </article>
    );
}
