"use client";

import {useState} from "react";
import Link from "next/link";
import {contactInitials, displayContactName} from "@/lib/contact-display";
import {formatMonth, relativeDate} from "@/lib/date-utils";
import {DeleteButton} from "@/components/DeleteButton";

export interface ProjectSummary {
    name: string;
    slug: string;
    status: string;
    startedAt: string;
    messageCount: number;
    dateRange: { first: string; last: string };
    members: { canonical_name: string; role: string; primary_email?: string; avatar_url?: string }[];
    summary: string;
    semanticConfidence?: string;
    aliases: string[];
}

export default function ProjectsClient({initialProjects}: { initialProjects: ProjectSummary[] }) {
    const [projects, setProjects] = useState(initialProjects);

    async function deleteProject(slug: string) {
        await fetch(`/api/projects/${slug}`, {method: "DELETE"});
        setProjects((prev) => prev.filter((p) => p.slug !== slug));
    }

    if (projects.length === 0) {
        return (
            <div
                className="text-center py-16 border border-dashed border-outline-variant rounded-2xl bg-surface-container-low">
                <span className="material-symbols-outlined text-5xl text-on-surface-variant/40 mb-3">folder_off</span>
                <h3 className="text-headline-md font-headline-md text-on-surface-variant font-bold mb-2">No Projects
                    Found</h3>
                <p className="text-ui-medium text-on-surface-variant max-w-md mx-auto">
                    No project files were detected in the archive.
                </p>
            </div>
        );
    }

    return (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            {projects.map((project) => {
                const isPlaceholder = project.status === "proposed";
                const isCompleted = project.status === "completed";
                const isDormant = project.status === "dormant";
                const isActive = project.status === "active";

                return (
                    <Link
                        key={project.slug}
                        href={`/projects/${project.slug}`}
                        className={`group relative flex flex-col justify-between p-6 bg-surface-container-low rounded-2xl border transition-all duration-300 hover:-translate-y-1 hover:shadow-md cursor-pointer ${
                            isPlaceholder
                                ? "border-dashed border-outline-variant/80 hover:border-outline"
                                : isCompleted
                                    ? "border-outline-variant/40 bg-surface-container-low/60 hover:border-outline-variant"
                                    : "border-outline-variant/40 hover:border-outline-variant hover:bg-white"
                        }`}
                    >
                        <div>
                            <div className="flex items-center justify-between mb-4">
                                {isActive && (
                                    <>
                                        <span
                                            className="bg-primary text-white px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">Active</span>
                                        <div className="flex items-center gap-2">
                      <span className="text-[11px] text-on-surface-variant font-medium">
                        Last entry: {project.dateRange.last ? relativeDate(project.dateRange.last) : ""}
                      </span>
                                            <DeleteButton onDelete={() => deleteProject(project.slug)}/>
                                        </div>
                                    </>
                                )}
                                {isDormant && (
                                    <>
                                        <span
                                            className="bg-surface-container-highest text-on-surface-variant px-2.5 py-0.5 rounded border border-outline-variant/30 text-[10px] font-mono uppercase font-bold tracking-wider">Dormant</span>
                                        <div className="flex items-center gap-2">
                      <span className="text-[11px] text-on-surface-variant font-medium">
                        {project.dateRange.last ? formatMonth(project.dateRange.last) : ""}
                      </span>
                                            <DeleteButton onDelete={() => deleteProject(project.slug)}/>
                                        </div>
                                    </>
                                )}
                                {isPlaceholder && (
                                    <>
                                        <span
                                            className="border border-dashed border-outline text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">Proposed</span>
                                        <div className="flex items-center gap-2">
                                            <span
                                                className="text-[11px] text-primary font-bold hover:underline transition-colors">Review Cluster</span>
                                            <DeleteButton onDelete={() => deleteProject(project.slug)}/>
                                        </div>
                                    </>
                                )}
                                {isCompleted && (
                                    <>
                                        <span
                                            className="border border-outline-variant bg-surface-container text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">Completed</span>
                                        <div className="flex items-center gap-2">
                                            <span
                                                className="material-symbols-outlined text-on-surface-variant/60 text-sm">lock</span>
                                            <DeleteButton onDelete={() => deleteProject(project.slug)}/>
                                        </div>
                                    </>
                                )}
                            </div>

                            <h2 className="text-headline-md font-headline-md text-primary font-bold mb-2 group-hover:text-primary-container transition-colors">
                                {project.name}
                            </h2>

                            {project.summary && (
                                <p className="text-ui-medium text-on-surface-variant line-clamp-3 mb-6 font-serif italic opacity-95">
                                    &ldquo;{project.summary}&rdquo;
                                </p>
                            )}
                        </div>

                        <div className="border-t border-outline-variant/40 pt-4 mt-auto">
                            {isActive && project.members.length > 0 && (
                                <div className="flex items-center justify-between">
                                    <div className="flex -space-x-1.5">
                                        {project.members.slice(0, 3).map((member, idx) => (
                                            <div
                                                key={idx}
                                                title={`${displayContactName(member.canonical_name, member.primary_email)} (${member.role})`}
                                                className="w-6 h-6 rounded-full bg-primary-fixed text-on-primary-fixed-variant text-[10px] font-bold border border-background flex items-center justify-center select-none overflow-hidden"
                                            >
                                                {member.avatar_url
                                                    ? <img src={member.avatar_url}
                                                           alt={displayContactName(member.canonical_name, member.primary_email)}
                                                           className="w-full h-full object-cover"/>
                                                    : contactInitials(displayContactName(member.canonical_name, member.primary_email))
                                                }
                                            </div>
                                        ))}
                                        {project.members.length > 3 && (
                                            <div
                                                className="w-6 h-6 rounded-full bg-primary text-white text-[10px] font-bold border border-background flex items-center justify-center select-none">
                                                +{project.members.length - 3}
                                            </div>
                                        )}
                                    </div>
                                    <span
                                        className="material-symbols-outlined text-primary text-lg opacity-0 group-hover:opacity-100 transition-opacity">arrow_forward</span>
                                </div>
                            )}

                            {isDormant && project.aliases.length > 0 && (
                                <div className="flex flex-wrap gap-1.5">
                                    {project.aliases.slice(0, 3).map((alias, idx) => (
                                        <span key={idx}
                                              className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold">{alias}</span>
                                    ))}
                                </div>
                            )}

                            {isPlaceholder && project.semanticConfidence && (
                                <div className="w-full space-y-1.5">
                                    <div className="w-full bg-surface-container h-1.5 rounded-full overflow-hidden">
                                        <div className="bg-primary h-full transition-all duration-500"
                                             style={{width: project.semanticConfidence}}/>
                                    </div>
                                    <p className="text-[10px] text-on-surface-variant font-mono font-bold uppercase tracking-wider">
                                        {project.semanticConfidence} Semantic Confidence
                                    </p>
                                </div>
                            )}

                            {isCompleted && (
                                <div
                                    className="flex items-center text-primary font-bold text-ui-small group-hover:underline">
                                    <span>View Archive</span>
                                    <span className="material-symbols-outlined text-sm ml-1">arrow_forward</span>
                                </div>
                            )}
                        </div>
                    </Link>
                );
            })}
        </div>
    );
}
