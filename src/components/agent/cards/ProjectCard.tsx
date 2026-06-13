"use client";
import {Calendar, FolderGit2, Users} from "lucide-react";
import {cleanCardText} from "./card-text";

interface Member {
    person_id: number;
    canonical_name: string;
    primary_email: string;
    role: string;
    slug: string;
}

interface NarrativeSection {
    content: string;
    source_message_ids: number[];
}

interface ProjectCardProps {
    data: {
        project_id: number;
        slug: string;
        name: string;
        status: string;
        started_at?: string;
        members?: Member[];
        narrative?: Record<string, NarrativeSection>;
        message_count: number;
    };
}

export default function ProjectCard({data}: ProjectCardProps) {
    const {
        name,
        slug,
        status,
        started_at,
        members = [],
        narrative = {},
        message_count,
    } = data;

    return (
        <div className="space-y-6 animate-fade-in">
            {/* Header Info */}
            <div
                className="relative overflow-hidden rounded-xl border border-outline-variant/60 bg-gradient-to-br from-primary/5 to-primary-fixed/5 p-5 shadow-sm">
                <div className="flex items-start gap-4">
                    <div
                        className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-primary-fixed text-on-primary-fixed-variant shadow-sm">
                        <FolderGit2 className="h-6 w-6"/>
                    </div>
                    <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex items-center flex-wrap gap-2">
                            <h2 className="text-xl font-bold tracking-tight text-on-surface">
                                {name}
                            </h2>
                            {status && (
                                <span
                                    className="inline-flex items-center rounded-full bg-primary/10 px-2.5 py-0.5 text-xs font-semibold text-primary capitalize">
                  {status}
                </span>
                            )}
                        </div>
                        <p className="text-xs text-on-surface-variant font-mono">
                            slug: {slug}
                        </p>
                        {started_at && (
                            <p className="flex items-center gap-1.5 text-xs text-on-surface-variant/80">
                                <Calendar className="h-3.5 w-3.5 shrink-0"/>
                                <span>Started: {started_at}</span>
                            </p>
                        )}
                    </div>
                </div>

                {/* Stats Grid */}
                <div className="mt-5 grid grid-cols-2 gap-2.5 border-t border-outline-variant/30 pt-4">
                    <div className="rounded bg-surface-container-low p-2.5 text-center">
                        <div
                            className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Messages
                        </div>
                        <div className="mt-1 text-lg font-bold text-on-surface">{message_count}</div>
                    </div>
                    <div className="rounded bg-surface-container-low p-2.5 text-center">
                        <div
                            className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Members
                        </div>
                        <div className="mt-1 text-lg font-bold text-on-surface">{members.length}</div>
                    </div>
                </div>
            </div>

            {/* Narrative Section */}
            {Object.keys(narrative).length > 0 && (
                <section className="space-y-4">
                    <h3 className="text-sm font-bold uppercase tracking-wider text-on-surface-variant">Narrative</h3>
                    <div className="space-y-4">
                        {narrative.summary && narrative.summary.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-4 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Executive Summary</h4>
                                <p className="text-sm leading-relaxed text-on-surface">{cleanCardText(narrative.summary.content)}</p>
                            </div>
                        )}
                        {narrative.phases && narrative.phases.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-4 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Project Phases</h4>
                                <p className="text-sm leading-relaxed text-on-surface whitespace-pre-wrap">{cleanCardText(narrative.phases.content)}</p>
                            </div>
                        )}
                        {narrative.friction_points && narrative.friction_points.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-4 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Friction Points</h4>
                                <p className="text-sm leading-relaxed text-on-surface whitespace-pre-wrap">{cleanCardText(narrative.friction_points.content)}</p>
                            </div>
                        )}
                        {narrative.current_understanding && narrative.current_understanding.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-4 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Current Understanding</h4>
                                <p className="text-sm leading-relaxed text-on-surface whitespace-pre-wrap">{cleanCardText(narrative.current_understanding.content)}</p>
                            </div>
                        )}
                    </div>
                </section>
            )}

            {/* Project Members */}
            {members.length > 0 && (
                <section className="space-y-3">
                    <h3 className="flex items-center gap-1.5 text-sm font-bold uppercase tracking-wider text-on-surface-variant">
                        <Users className="h-4 w-4"/>
                        Project Members
                    </h3>
                    <ul className="space-y-2">
                        {members.map((m) => (
                            <li
                                key={m.person_id}
                                className="flex items-center justify-between gap-3 rounded-lg border border-outline-variant/35 bg-surface-container-low/40 p-3"
                            >
                                <div className="min-w-0">
                                    <div className="truncate text-xs font-bold text-on-surface">{m.canonical_name}</div>
                                    <div
                                        className="truncate text-[10px] text-on-surface-variant font-mono">{m.primary_email}</div>
                                </div>
                                {m.role && (
                                    <span
                                        className="shrink-0 rounded bg-primary-fixed/30 border border-primary-fixed-variant/20 px-2 py-1 text-[10px] font-semibold text-on-primary-fixed-variant">
                    {m.role}
                  </span>
                                )}
                            </li>
                        ))}
                    </ul>
                </section>
            )}
        </div>
    );
}
