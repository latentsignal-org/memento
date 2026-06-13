"use client";

import {useState} from "react";
import Link from "next/link";
import {DeleteButton} from "@/components/DeleteButton";

export interface ConceptIndexEntry {
    slug: string;
    name: string;
    scope_description: string;
    status: string;
    message_count: number;
    has_narrative: boolean;
}

export default function ConceptsClient({initialConcepts}: { initialConcepts: ConceptIndexEntry[] }) {
    const [concepts, setConcepts] = useState(initialConcepts);

    async function deleteConcept(slug: string) {
        await fetch(`/api/concepts/${slug}`, {method: "DELETE"});
        setConcepts((prev) => prev.filter((c) => c.slug !== slug));
    }

    if (concepts.length === 0) {
        return (
            <div
                className="rounded-2xl border border-dashed border-outline-variant bg-surface-container-low py-16 text-center">
                <span className="material-symbols-outlined mb-3 text-5xl text-on-surface-variant/40">lightbulb</span>
                <h3 className="mb-2 text-headline-md font-bold font-headline-md text-on-surface-variant">No Concepts
                    Found</h3>
                <p className="text-ui-medium mx-auto max-w-md text-on-surface-variant">
                    Declare a concept to build the first evergreen knowledge page from your archive.
                </p>
            </div>
        );
    }

    return (
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            {concepts.map((c) => (
                <Link
                    key={c.slug}
                    href={`/concepts/${c.slug}`}
                    className="group relative flex cursor-pointer flex-col justify-between rounded-2xl border border-outline-variant/40 bg-surface-container-low p-6 transition-all duration-300 hover:-translate-y-1 hover:border-outline-variant hover:bg-white hover:shadow-md"
                >
                    <div>
                        <div className="mb-4 flex items-center justify-between">
              <span
                  className="shrink-0 rounded bg-primary-fixed px-2.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-on-primary-fixed">
                {c.has_narrative ? "Curated" : "Pending"}
              </span>
                            <DeleteButton onDelete={() => deleteConcept(c.slug)}/>
                        </div>

                        <h2 className="mb-2 text-headline-md font-bold font-headline-md text-primary transition-colors group-hover:text-primary-container">
                            {c.name}
                        </h2>
                        <p className="text-ui-medium mb-6 line-clamp-3 leading-relaxed text-on-surface-variant">
                            {c.scope_description || "(no scope description)"}
                        </p>
                    </div>

                    <div
                        className="mt-auto flex items-center justify-between border-t border-outline-variant/40 pt-4 text-[11px] text-on-surface-variant">
                        <span className="font-bold">{c.message_count.toLocaleString()} sources</span>
                        <span className="font-mono font-bold uppercase tracking-wide">{c.status}</span>
                    </div>
                </Link>
            ))}
        </div>
    );
}
