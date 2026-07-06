"use client";

import {useEffect, useState} from "react";
import PersonDetailClient from "./PersonDetailClient";
import {displayContactName} from "@/lib/contact-display";
import {avatarUrl, initialsFromName} from "@/lib/avatar";
import {apiGet, currentSlug, privacyEnabled} from "@/lib/api";
import {type SimulationFlags, simulationFromLocation} from "@/lib/static-page";
import EntityNotFound from "@/components/EntityNotFound";
import type {PersonNetwork, PersonRecord} from "./types";

async function loadPerson(slug: string): Promise<PersonRecord | null> {
    const match = await apiGet<PersonRecord>(`/api/people/${slug}`);
    if (!match) return null;
    return {
        ...match,
        avatar_url: avatarUrl(match.primary_email, 160, initialsFromName(match.canonical_name, match.primary_email)),
        top_correspondents: (match.top_correspondents || []).map((c) => ({
            ...c,
            avatar_url: avatarUrl(c.primary_email, 64, initialsFromName(c.canonical_name, c.primary_email)),
        })),
        slug,
    };
}

async function loadPersonNetwork(slug: string): Promise<PersonNetwork | null> {
    return apiGet<PersonNetwork>(`/api/people/${slug}/network?limit=10`);
}

export default function PersonDetailPageClient() {
    const [state, setState] = useState<
        { person: PersonRecord; network: PersonNetwork | null } | null | undefined
    >(undefined);
    const [sim, setSim] = useState<SimulationFlags>({simulationMode: false, simulationDelayMs: null});

    useEffect(() => {
        let cancelled = false;
        const slug = currentSlug();
        setSim(simulationFromLocation());
        Promise.all([loadPerson(slug), loadPersonNetwork(slug)]).then(([person, network]) => {
            if (cancelled) return;
            if (!person) {
                setState(null);
                return;
            }
            const displayName = displayContactName(
                person.canonical_name,
                person.primary_email,
                privacyEnabled(),
            );
            document.title = `${displayName} | Memento`;
            setState({person, network});
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (state === undefined) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    if (state === null) {
        return <EntityNotFound kind="person" backHref="/people" backLabel="Back to People"/>;
    }

    return (
        <main className="pt-16 min-h-screen bg-background">
            <PersonDetailClient
                person={state.person}
                network={state.network}
                simulationMode={sim.simulationMode}
                simulationDelayMs={sim.simulationDelayMs}
            />
        </main>
    );
}
