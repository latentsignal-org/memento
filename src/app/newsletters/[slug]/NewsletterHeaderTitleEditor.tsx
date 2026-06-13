"use client";

import {useState} from "react";
import {useRouter} from "next/navigation";
import {revalidateEntityPath} from "@/app/actions";
import {maskEmailAddresses} from "@/lib/contact-display";

export default function NewsletterHeaderTitleEditor({
                                                        slug,
                                                        initialTitle,
                                                    }: {
    slug: string;
    initialTitle: string;
}) {
    const router = useRouter();
    const [isEditing, setIsEditing] = useState(false);
    const [draft, setDraft] = useState(initialTitle);
    const [title, setTitle] = useState(initialTitle);
    const [error, setError] = useState<string | null>(null);

    async function save() {
        const name = draft.trim();
        if (!name || name === title) {
            setIsEditing(false);
            setDraft(title);
            return;
        }
        setError(null);
        const res = await fetch(`/api/newsletters/${slug}`, {
            method: "PATCH",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({name}),
        });
        if (!res.ok) {
            setError("Could not save title. Make sure the Go server is restarted.");
            return;
        }
        setTitle(name);
        setIsEditing(false);
        void revalidateEntityPath(`/newsletters/${slug}`)
            .catch((error) => console.error("revalidate newsletter path", error))
            .finally(() => {
                window.location.reload();
            });
    }

    if (isEditing) {
        return (
            <div className="space-y-1">
                <div className="flex w-full max-w-5xl min-w-0 items-center gap-3">
                    <input
                        autoFocus
                        value={draft}
                        onChange={(e) => setDraft(e.target.value)}
                        className="min-w-0 flex-1 rounded border border-outline-variant px-3 py-2 text-headline-md"
                    />
                    <div className="flex items-center gap-2 whitespace-nowrap">
                        <button type="button" onClick={() => void save()}
                                className="text-ui-small font-bold text-primary">Save
                        </button>
                        <button type="button" onClick={() => {
                            setIsEditing(false);
                            setDraft(title);
                        }} className="text-ui-small text-on-surface-variant">Cancel
                        </button>
                    </div>
                </div>
                {error ? <p className="text-[12px] text-error">{error}</p> : null}
            </div>
        );
    }

    return (
        <h1 className="text-display-lg font-display-lg text-primary tracking-tight leading-tight flex items-center gap-2">
            <span>{maskEmailAddresses(title)}</span>
            <button type="button" onClick={() => setIsEditing(true)}
                    className="text-on-surface-variant hover:text-primary">
                <span className="material-symbols-outlined text-base">edit</span>
            </button>
        </h1>
    );
}
