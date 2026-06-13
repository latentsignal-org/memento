"use client";

import {useEffect, useState} from "react";
import Link from "next/link";
import {apiGet} from "@/lib/api";
import ConceptsClient, {type ConceptIndexEntry} from "./ConceptsClient";
import HowConceptsWork from "./HowConceptsWork";

export default function ConceptsPageClient() {
    const [concepts, setConcepts] = useState<ConceptIndexEntry[] | null>(null);

    useEffect(() => {
        let cancelled = false;
        apiGet<{ concepts?: ConceptIndexEntry[] }>("/api/concepts").then((data) => {
            if (!cancelled) setConcepts(data?.concepts || []);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (concepts === null) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    const activeCount = concepts.filter((concept) => concept.status === "active").length;

    return (
        <main className="min-h-screen bg-background pt-16 text-on-surface">
            <div
                className="mx-auto grid w-full max-w-[1440px] grid-cols-1 gap-8 px-4 py-8 sm:px-6 sm:py-12 lg:grid-cols-12">
                <section className="space-y-8 lg:col-span-9">
                    <header className="space-y-4">
                        <div className="flex flex-wrap items-center gap-4">
                            <h1 className="text-display-lg font-display-lg text-primary tracking-tight max-sm:text-[32px]">
                                Concepts
                            </h1>
                            <div className="mt-2 flex items-center gap-2 sm:mt-0">
                <span
                    className="rounded border border-outline-variant/60 bg-surface-container-low px-2.5 py-0.5 font-mono text-[11px] font-bold text-on-surface-variant">
                  {concepts.length} TOTAL
                </span>
                                <span
                                    className="rounded bg-primary-fixed px-2.5 py-0.5 font-mono text-[11px] font-bold text-on-primary-fixed-variant">
                  {activeCount} ACTIVE
                </span>
                            </div>
                            <Link
                                href="/concepts/new"
                                className="ml-auto rounded bg-primary px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                            >
                                + New concept
                            </Link>
                        </div>
                        <p className="text-body-reading font-body-reading max-w-[800px] leading-relaxed text-on-surface-variant">
                            Evergreen knowledge, curated from your archive.
                        </p>
                    </header>

                    <ConceptsClient initialConcepts={concepts}/>
                </section>

                <aside className="space-y-6 lg:col-span-3">
                    <HowConceptsWork/>
                </aside>
            </div>

            <footer className="mt-20 w-full border-t border-outline-variant/40 bg-surface-container-low/40 py-12">
                <div className="mx-auto max-w-[1440px] px-4 sm:px-6">
          <span className="mb-4 block text-label-caps font-bold text-primary">
            DIMENSIONAL MEMORY
          </span>
                    <div
                        className="text-ui-medium max-w-[620px] font-display-lg italic leading-relaxed text-on-surface">
                        &ldquo;Concepts preserve evergreen topics as living knowledge pages, connecting repeat ideas
                        across projects, newsletters, and people.&rdquo;
                    </div>
                </div>
            </footer>
        </main>
    );
}
