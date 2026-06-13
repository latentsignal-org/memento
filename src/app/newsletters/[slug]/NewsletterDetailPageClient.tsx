"use client";

import {useEffect, useState} from "react";
import Link from "next/link";
import {maskEmailAddresses} from "@/lib/contact-display";
import {formatMonthDay} from "@/lib/date-utils";
import NewsletterDetailClient from "./NewsletterDetailClient";
import NewsletterHeaderTitleEditor from "./NewsletterHeaderTitleEditor";
import {apiGet, currentSlug, privacyEnabled} from "@/lib/api";
import {type SimulationFlags, simulationFromLocation} from "@/lib/static-page";
import EmailReveal from "@/components/email-reveal";
import EntityNotFound from "@/components/EntityNotFound";

interface NewsletterPageData {
    source: {
        slug: string;
        display_name: string;
        sender_email: string;
        domain: string;
        message_count: number;
        first_seen?: string;
        last_seen?: string;
        classification_reason: string;
    };
    narrative: {
        coverage_summary?: string;
        recurring_themes?: { theme: string; source_message_ids: number[] }[];
        notable_recent?: { headline: string; date: string; source_message_ids: number[] }[];
    };
    timeline: {
        message_id: number;
        sent_at: string;
        subject: string;
        snippet: string;
        body_text?: string;
    }[];
}

export default function NewsletterDetailPageClient() {
    const [data, setData] = useState<NewsletterPageData | null | undefined>(undefined);
    const [sim, setSim] = useState<SimulationFlags>({simulationMode: false, simulationDelayMs: null});

    useEffect(() => {
        let cancelled = false;
        const slug = currentSlug();
        setSim(simulationFromLocation());
        apiGet<NewsletterPageData>(`/api/newsletters/${slug}`).then((result) => {
            if (cancelled) return;
            if (!result) {
                setData(null);
                return;
            }
            const displayName = maskEmailAddresses(result.source.display_name, privacyEnabled());
            document.title = `${displayName} | Memento`;
            setData(result);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (data === undefined) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    if (data === null) {
        return <EntityNotFound kind="newsletter" backHref="/newsletters" backLabel="Back to Newsletters"/>;
    }

    const source = data.source;

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div className="w-full max-w-[1280px] mx-auto px-6 py-12">
                <Link href="/newsletters"
                      className="inline-flex items-center gap-2 text-ui-small text-primary hover:underline mb-8">
                    <span className="material-symbols-outlined text-sm">arrow_back</span>
                    Newsletters
                </Link>

                <header className="border-b border-outline-variant pb-8 mb-10">
                    <div className="flex flex-col lg:flex-row lg:items-end lg:justify-between gap-6">
                        <div className="space-y-3 flex-1 min-w-0">
                            <div className="flex flex-wrap gap-2">
                <span
                    className="bg-primary text-white px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">
                  Newsletter
                </span>
                                <span
                                    className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold">
                  {source.message_count} messages
                </span>
                            </div>
                            <NewsletterHeaderTitleEditor slug={source.slug} initialTitle={source.display_name}/>
                            <div className="mt-1">
                                <EmailReveal email={source.sender_email}
                                             className="text-ui-medium text-on-surface-variant font-mono"/>
                            </div>
                        </div>
                        <div className="text-left lg:text-right text-ui-small text-on-surface-variant">
                            <p>{formatMonthDay(source.first_seen)} - {formatMonthDay(source.last_seen)}</p>
                            <p>{source.classification_reason}</p>
                        </div>
                    </div>
                </header>

                <NewsletterDetailClient
                    data={data}
                    simulationMode={sim.simulationMode}
                    simulationDelayMs={sim.simulationDelayMs}
                />
            </div>
        </main>
    );
}
