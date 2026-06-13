"use client";

import {useState} from "react";
import Link from "next/link";
import {useRouter} from "next/navigation";
import {
    Archive,
    ChevronDown,
    FilePlus2,
    MessageSquare,
    MoreHorizontal,
    Pencil,
    Pin,
    PinOff,
    Trash2,
} from "lucide-react";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

interface SessionActionsProps {
    slug: string;
    pinned: boolean;
    archived: boolean;
}

export default function SessionActions({
                                           slug,
                                           pinned,
                                           archived,
                                       }: SessionActionsProps) {
    const router = useRouter();
    const [busy, setBusy] = useState<string | null>(null);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const [deleteError, setDeleteError] = useState<string | null>(null);

    async function patchSession(body: Record<string, unknown>, label: string) {
        setBusy(label);
        try {
            const res = await fetch(`/api/sessions/${encodeURIComponent(slug)}`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify(body),
            });
            if (!res.ok) throw new Error(await res.text());
            window.location.reload();
        } catch (error) {
            alert(error instanceof Error ? error.message : String(error));
        } finally {
            setBusy(null);
        }
    }

    async function promote(kind: "project" | "concept") {
        setBusy(kind);
        try {
            const res = await fetch(`/api/sessions/${encodeURIComponent(slug)}/promote`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({kind}),
            });
            if (!res.ok) throw new Error(await res.text());
            const data = (await res.json()) as { url?: string };
            router.push(data.url || (kind === "project" ? "/projects/new" : "/concepts/new"));
        } catch (error) {
            alert(error instanceof Error ? error.message : String(error));
        } finally {
            setBusy(null);
        }
    }

    async function deleteSession() {
        setBusy("delete");
        setDeleteError(null);
        try {
            const res = await fetch(`/api/sessions/${encodeURIComponent(slug)}`, {
                method: "DELETE",
            });
            if (!res.ok) throw new Error(await res.text());
            router.push("/sessions");
        } catch (error) {
            setDeleteError(error instanceof Error ? error.message : String(error));
        } finally {
            setBusy(null);
        }
    }

    const disabled = busy !== null;

    return (
        <>
            <div className="flex flex-wrap items-center justify-end gap-2">
                <Link
                    href={`/ask?session=${encodeURIComponent(slug)}`}
                    className="inline-flex items-center gap-1.5 rounded bg-primary px-3 py-2 text-xs font-semibold text-white shadow-sm transition hover:opacity-90"
                >
                    <MessageSquare className="h-3.5 w-3.5"/>
                    Continue in Ask
                </Link>

                <DropdownMenu>
                    <DropdownMenuTrigger
                        disabled={disabled}
                        className="inline-flex items-center gap-1.5 rounded border border-primary/20 bg-primary-fixed px-3 py-2 text-xs font-semibold text-primary transition hover:opacity-85 disabled:opacity-50"
                    >
                        <FilePlus2 className="h-3.5 w-3.5"/>
                        Continue in...
                        <ChevronDown className="h-3.5 w-3.5"/>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="min-w-44">
                        <DropdownMenuItem onClick={() => void promote("project")} disabled={disabled}>
                            <FilePlus2 className="h-4 w-4"/>
                            New Project
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => void promote("concept")} disabled={disabled}>
                            <FilePlus2 className="h-4 w-4"/>
                            New Concept
                        </DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>

                <DropdownMenu>
                    <DropdownMenuTrigger
                        disabled={disabled}
                        className="inline-flex items-center gap-1.5 rounded border border-outline-variant/60 bg-background px-3 py-2 text-xs font-semibold text-on-surface-variant transition hover:text-primary disabled:opacity-50"
                    >
                        <MoreHorizontal className="h-3.5 w-3.5"/>
                        More
                        <ChevronDown className="h-3.5 w-3.5"/>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="min-w-36">
                        <DropdownMenuItem onClick={() => void patchSession({pinned: !pinned}, "pin")}
                                          disabled={disabled}>
                            {pinned ? <PinOff className="h-4 w-4"/> : <Pin className="h-4 w-4"/>}
                            {pinned ? "Unpin" : "Pin"}
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => void patchSession({archived: !archived}, "archive")}
                                          disabled={disabled}>
                            <Archive className="h-4 w-4"/>
                            {archived ? "Unarchive" : "Archive"}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator/>
                        <DropdownMenuItem
                            onClick={() => {
                                setDeleteError(null);
                                setConfirmDelete(true);
                            }}
                            disabled={disabled}
                            variant="destructive"
                        >
                            <Trash2 className="h-4 w-4"/>
                            Delete
                        </DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>
            </div>

            {confirmDelete ? (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/20 px-4">
                    <div
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="delete-session-title"
                        className="w-full max-w-sm rounded-lg border border-outline-variant/60 bg-background p-5 shadow-xl"
                    >
                        <h2 id="delete-session-title" className="text-lg font-semibold text-on-surface">
                            Delete this session?
                        </h2>
                        <p className="mt-2 text-sm leading-6 text-on-surface-variant">
                            This removes the saved session from Sessions. Raw debug runs remain available in Debug.
                        </p>
                        {deleteError ? (
                            <p className="mt-3 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                                {deleteError}
                            </p>
                        ) : null}
                        <div className="mt-5 flex justify-end gap-2">
                            <button
                                type="button"
                                onClick={() => setConfirmDelete(false)}
                                disabled={disabled}
                                className="rounded border border-outline-variant/60 bg-background px-3 py-2 text-sm font-semibold text-on-surface-variant transition hover:text-primary disabled:opacity-50"
                            >
                                No
                            </button>
                            <button
                                type="button"
                                onClick={() => void deleteSession()}
                                disabled={disabled}
                                className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm font-semibold text-red-700 transition hover:bg-red-100 disabled:opacity-50"
                            >
                                Yes, delete
                            </button>
                        </div>
                    </div>
                </div>
            ) : null}
        </>
    );
}

export function SessionTitleEditor({
                                       slug,
                                       title,
                                   }: {
    slug: string;
    title: string;
}) {
    const router = useRouter();
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState(title);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);

    async function saveTitle() {
        const next = draft.trim();
        if (!next || next === title.trim()) {
            setDraft(title);
            setEditing(false);
            setError(null);
            return;
        }
        setBusy(true);
        setError(null);
        try {
            const res = await fetch(`/api/sessions/${encodeURIComponent(slug)}`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({title: next}),
            });
            if (!res.ok) throw new Error(await res.text());
            setEditing(false);
            window.location.reload();
        } catch (error) {
            setError(error instanceof Error ? error.message : String(error));
        } finally {
            setBusy(false);
        }
    }

    function cancelEdit() {
        setDraft(title);
        setEditing(false);
        setError(null);
    }

    if (editing) {
        return (
            <div className="w-full">
                <div className="flex w-full max-w-5xl min-w-0 items-center gap-3">
                    <input
                        value={draft}
                        onChange={(event) => setDraft(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === "Enter") {
                                event.preventDefault();
                                void saveTitle();
                            }
                            if (event.key === "Escape") {
                                event.preventDefault();
                                cancelEdit();
                            }
                        }}
                        autoFocus
                        disabled={busy}
                        className="min-w-0 flex-1 rounded border border-outline-variant bg-background px-3 py-2 text-headline-md text-on-surface outline-none ring-primary/20 focus:border-primary/50 focus:ring-2 disabled:opacity-60"
                        aria-label="Session title"
                    />
                    <div className="flex items-center gap-2 whitespace-nowrap">
                        <button
                            type="button"
                            onClick={() => void saveTitle()}
                            disabled={busy}
                            className="text-ui-small font-bold text-primary transition hover:opacity-80 disabled:opacity-50"
                        >
                            Save
                        </button>
                        <button
                            type="button"
                            onClick={cancelEdit}
                            disabled={busy}
                            className="text-ui-small text-on-surface-variant transition hover:text-primary disabled:opacity-50"
                        >
                            Cancel
                        </button>
                    </div>
                </div>
                {error ? <p className="mt-2 text-sm text-red-700">{error}</p> : null}
            </div>
        );
    }

    return (
        <div className="flex max-w-[920px] items-start gap-2">
            <h1 className="min-w-0 text-display-lg font-display-lg text-primary tracking-tight max-sm:text-[34px]">
                {title}
            </h1>
            <button
                type="button"
                onClick={() => {
                    setDraft(title);
                    setEditing(true);
                }}
                disabled={busy}
                className="mt-2 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-on-surface-variant transition hover:bg-primary-fixed/40 hover:text-primary disabled:opacity-50"
                aria-label="Rename session"
                title="Rename session"
            >
                <Pencil className="h-4 w-4"/>
            </button>
        </div>
    );
}
