"use client";

import {useEffect, useState} from "react";
import ProjectDetailClient from "./ProjectDetailClient";
import {avatarUrl, initialsFromName} from "@/lib/avatar";
import {apiGet, currentSlug} from "@/lib/api";
import {type SimulationFlags, simulationFromLocation} from "@/lib/static-page";
import EntityNotFound from "@/components/EntityNotFound";

interface ProjectMemberPayload {
    primary_email?: string;
    canonical_name?: string;
    [k: string]: unknown;
}

interface ProjectPayload {
    name?: string;
    members?: ProjectMemberPayload[];
    narrative?: { summary?: string };

    [k: string]: unknown;
}

export default function ProjectDetailPageClient() {
    const [project, setProject] = useState<ProjectPayload | null | undefined>(undefined);
    const [sim, setSim] = useState<SimulationFlags>({simulationMode: false, simulationDelayMs: null});

    useEffect(() => {
        let cancelled = false;
        const slug = currentSlug();
        setSim(simulationFromLocation());
        apiGet<ProjectPayload>(`/api/projects/${slug}`).then((data) => {
            if (cancelled) return;
            if (!data) {
                setProject(null);
                return;
            }
            data.timeline = data.timeline || [];
            data.members = (data.members || []).map((member) => ({
                ...member,
                avatar_url: avatarUrl(member.primary_email || "", 64, initialsFromName(member.canonical_name, member.primary_email)),
            }));
            if (data.name) document.title = `${data.name} | Memento`;
            setProject(data);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (project === undefined) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    if (project === null) {
        return <EntityNotFound kind="project" backHref="/projects" backLabel="Back to Projects"/>;
    }

    return (
        <main className="pt-16 min-h-screen bg-background">
            <ProjectDetailClient
                project={project as never}
                simulationMode={sim.simulationMode}
                simulationDelayMs={sim.simulationDelayMs}
            />
        </main>
    );
}
