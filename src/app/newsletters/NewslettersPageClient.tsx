"use client";

import {useEffect, useState} from "react";
import {apiGet} from "@/lib/api";
import NewslettersClient from "./NewslettersClient";

interface NewsletterIndexSource {
    slug: string;
    display_name: string;
    sender_email: string;
    domain: string;
    message_count: number;
    unsubscribe_count: number;
    first_seen?: string;
    last_seen?: string;
    classification_reason: string;
    recent_subjects: string[];
}

export default function NewslettersPageClient() {
    const [sources, setSources] = useState<NewsletterIndexSource[] | null>(null);

    useEffect(() => {
        let cancelled = false;
        apiGet<{ sources?: NewsletterIndexSource[] }>("/api/newsletters").then((data) => {
            if (!cancelled) setSources(data?.sources || []);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (sources === null) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    // Show every detected source — Morning Brew (~rank 30) and similar mid-
    // volume newsletters were invisible behind the previous 12-card cap.
    const topSources = sources;
    const totalMessages = sources.reduce((sum, source) => sum + source.message_count, 0);

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div className="w-full max-w-[1440px] mx-auto px-6 py-12 grid grid-cols-1 lg:grid-cols-12 gap-8">
                <section className="lg:col-span-9 space-y-8">
                    <header className="space-y-4">
                        <div className="flex flex-wrap items-center gap-4">
                            <h1 className="text-display-lg font-display-lg text-primary tracking-tight">
                                Newsletter Sources
                            </h1>
                            <div className="flex items-center gap-2 mt-2 sm:mt-0">
                <span
                    className="border border-outline-variant/60 bg-surface-container-low text-on-surface-variant font-mono px-2.5 py-0.5 rounded text-[11px] font-bold">
                  {sources.length} SOURCES
                </span>
                                <span
                                    className="bg-primary-fixed text-on-primary-fixed-variant font-mono px-2.5 py-0.5 rounded text-[11px] font-bold">
                  {totalMessages.toLocaleString()} EMAILS
                </span>
                            </div>
                        </div>
                        <p className="text-body-reading font-body-reading text-on-surface-variant max-w-[800px] leading-relaxed">
                            Broadcast sources detected from recurring unsubscribe signals and sender patterns. Each page
                            treats a newsletter email as one sourced unit for fast coverage synthesis.
                        </p>
                    </header>

                    {sources.length > 0 ? (
                        <NewslettersClient initialSources={topSources}/>
                    ) : (
                        <div
                            className="bg-surface-container-low border border-dashed border-outline-variant rounded-2xl p-10 text-center">
                            <h2 className="text-headline-md font-headline-md text-primary mb-2">No newsletters detected
                                yet</h2>
                            <p className="text-ui-medium text-on-surface-variant">
                                Run <code className="font-mono">memento init</code> to index your archive, then <code
                                className="font-mono">memento app</code> to start Memento.
                            </p>
                        </div>
                    )}
                </section>

                <aside className="lg:col-span-3 space-y-6">
                    <div className="bg-primary text-primary-foreground rounded-2xl p-6">
                        <h2 className="text-ui-medium font-bold mb-4">Coverage Snapshot</h2>
                        <div className="space-y-4">
                            <div>
                                <p className="text-[11px] uppercase tracking-wider opacity-70">Detected Sources</p>
                                <p className="text-headline-md font-headline-md">{sources.length}</p>
                            </div>
                            <div>
                                <p className="text-[11px] uppercase tracking-wider opacity-70">Newsletter Emails</p>
                                <p className="text-headline-md font-headline-md">{totalMessages.toLocaleString()}</p>
                            </div>
                        </div>
                    </div>

                    <div className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6">
                        <h2 className="text-ui-medium font-bold text-primary mb-4">Top Domains</h2>
                        <div className="space-y-3">
                            {Object.entries(
                                sources.reduce<Record<string, number>>((acc, source) => {
                                    acc[source.domain] = (acc[source.domain] || 0) + 1;
                                    return acc;
                                }, {})
                            )
                                .sort((a, b) => b[1] - a[1])
                                .slice(0, 8)
                                .map(([domain, count]) => (
                                    <div key={domain} className="flex items-center justify-between text-ui-small">
                                        <span className="text-on-surface-variant truncate">{domain}</span>
                                        <span className="font-mono text-primary font-bold">{count}</span>
                                    </div>
                                ))}
                        </div>
                    </div>
                </aside>
            </div>
        </main>
    );
}
