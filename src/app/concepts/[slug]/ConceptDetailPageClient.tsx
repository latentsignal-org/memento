"use client";

import {useEffect, useState} from "react";
import ConceptDetailClient from "./ConceptDetailClient";
import {apiGet, currentSlug, privacyEnabled} from "@/lib/api";
import {maskEmailAddresses} from "@/lib/contact-display";
import {type SimulationFlags, simulationFromLocation} from "@/lib/static-page";
import EntityNotFound from "@/components/EntityNotFound";

interface ConceptInsight {
    title: string;
    content: string;
    source_message_ids: number[];
}

interface ConceptPersonContribution {
    person_id: number;
    canonical_name: string;
    primary_email: string;
    slug: string;
    profile_slug?: string;
    has_profile: boolean;
    contributions: number;
}

interface ConceptNewsletterContribution {
    slug: string;
    display_name: string;
    sender_email: string;
    contributions: number;
}

interface ConceptTimelineItem {
    message_id: number;
    date: string;
    subject: string;
    from_canonical_name: string;
    snippet: string;
    is_newsletter: boolean;
    newsletter_slug?: string;
}

interface ConceptPage {
    concept_id: number;
    slug: string;
    name: string;
    scope_description: string;
    status: string;
    seed_keywords: string[];
    message_count: number;
    date_range: { first: string; last: string };
    source_map: {
        people: ConceptPersonContribution[];
        newsletters: ConceptNewsletterContribution[];
    };
    timeline: ConceptTimelineItem[];
    narrative: {
        scope_summary: string;
        distilled_insights: ConceptInsight[];
        evolving_understanding: string;
    };
}

export default function ConceptDetailPageClient() {
    const [concept, setConcept] = useState<ConceptPage | null | undefined>(undefined);
    const [sim, setSim] = useState<SimulationFlags>({simulationMode: false, simulationDelayMs: null});

    useEffect(() => {
        let cancelled = false;
        const slug = currentSlug();
        setSim(simulationFromLocation());
        apiGet<ConceptPage>(`/api/concepts/${slug}`).then((data) => {
            if (cancelled) return;
            if (!data) {
                setConcept(null);
                return;
            }
            const displayName = maskEmailAddresses(data.name, privacyEnabled());
            document.title = `${displayName} | Concepts | Memento`;
            setConcept(data);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (concept === undefined) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    if (concept === null) {
        return <EntityNotFound kind="concept" backHref="/concepts" backLabel="Back to Concepts"/>;
    }

    return (
        <ConceptDetailClient
            concept={concept}
            simulationMode={sim.simulationMode}
            simulationDelayMs={sim.simulationDelayMs}
        />
    );
}
