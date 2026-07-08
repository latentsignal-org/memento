"use client";

import {useEffect, useState} from "react";
import Link from "next/link";
import {avatarUrl, initialsFromName} from "@/lib/avatar";
import {apiGet} from "@/lib/api";
import HowProjectsWork from "./HowProjectsWork";
import ProjectsClient, {type ProjectSummary} from "./ProjectsClient";

interface ProjectsApiResponse {
    projects?: Array<{
        project_id: number;
        slug: string;
        name: string;
        aliases?: string[];
        status: string;
        started_at?: string;
        message_count: number;
        summary_excerpt?: string;
    }>;
}

async function getProjects(): Promise<ProjectSummary[]> {
    const index = await apiGet<ProjectsApiResponse>("/api/projects");
    const summaries = index?.projects || [];

    // For richer cards we hydrate per-project members + date_range from the
    // detail endpoint. Sequential is fine at hackathon scale (≤10 projects);
    // switch to a Promise.all if the list grows.
    const projects: ProjectSummary[] = [];
    for (const s of summaries) {
        const detail = await apiGet<{
            members?: Array<{ canonical_name: string; role: string; primary_email?: string }>;
            date_range?: { first: string; last: string };
            narrative?: { summary?: string };
            aliases?: string[];
        }>(`/api/projects/${s.slug}`);
        projects.push({
            name: s.name || "Unnamed Project",
            slug: s.slug,
            status: s.status || "active",
            startedAt: s.started_at || "",
            messageCount: s.message_count || 0,
            dateRange: detail?.date_range || {first: "", last: ""},
            members: (detail?.members || []).map((member) => ({
                ...member,
                avatar_url: avatarUrl(member.primary_email || "", 48, initialsFromName(member.canonical_name, member.primary_email)),
            })),
            summary: detail?.narrative?.summary || s.summary_excerpt || "",
            aliases: detail?.aliases || s.aliases || [],
        });
    }

    return projects.sort((a, b) => {
        if (a.status === "active" && b.status !== "active") return -1;
        if (a.status !== "active" && b.status === "active") return 1;
        return b.startedAt.localeCompare(a.startedAt);
    });
}

export default function ProjectsPageClient() {
    const [projects, setProjects] = useState<ProjectSummary[] | null>(null);

    useEffect(() => {
        let cancelled = false;
        getProjects().then((result) => {
            if (!cancelled) setProjects(result);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (projects === null) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    const activeCount = projects.filter((p) => p.status === "active").length;

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div
                className="w-full max-w-[1440px] mx-auto px-4 sm:px-6 py-8 sm:py-12 grid grid-cols-1 lg:grid-cols-12 gap-8">

                {/* Main Content Area: Project*/}
                <section className="lg:col-span-9 space-y-8">
                    {/* Header */}
                    <header className="space-y-4">
                        <div className="flex flex-wrap items-center gap-4">
                            <h1 className="text-display-lg font-display-lg text-primary tracking-tight max-sm:text-[32px]">
                                Projects
                            </h1>
                            <div className="flex items-center gap-2 mt-2 sm:mt-0">
                <span
                    className="border border-outline-variant/60 bg-surface-container-low text-on-surface-variant font-mono px-2.5 py-0.5 rounded text-[11px] font-bold">
                  {projects.length} TOTAL
                </span>
                                <span
                                    className="bg-primary-fixed text-on-primary-fixed-variant font-mono px-2.5 py-0.5 rounded text-[11px] font-bold">
                  {activeCount} ACTIVE
                </span>
                            </div>
                            <Link
                                href="/projects/new"
                                className="ml-auto px-4 py-2 rounded bg-primary text-white font-medium text-sm hover:opacity-90"
                            >
                                + New project
                            </Link>
                        </div>
                        <p className="text-body-reading font-body-reading text-on-surface-variant max-w-[800px] leading-relaxed">
                            Generated summary from time-bound correspondence.
                        </p>
                    </header>

                    {/* Grid of Project Cards */}
                    <ProjectsClient initialProjects={projects}/>
                </section>

                {/* Sidebar: How Projects Work */}
                <aside className="lg:col-span-3 space-y-6">
                    <HowProjectsWork/>
                </aside>

            </div>

            {/* Archival Footer */}
            <footer className="w-full border-t border-outline-variant/40 bg-surface-container-low/40 py-12 mt-20">
                <div className="max-w-[1440px] mx-auto px-4 sm:px-6">
                    <div>
            <span className="text-label-caps text-primary mb-4 block font-bold">
              DIMENSIONAL MEMORY
            </span>
                        <div
                            className="text-ui-medium text-on-surface italic font-display-lg leading-relaxed max-w-[500px]">
                            &ldquo;Projects group temporal signals into stories. A project acts as a boundary for life
                            milestones, isolating threads into a unified timeline.&rdquo;
                        </div>
                    </div>
                </div>
            </footer>
        </main>
    );
}
