"use client";
import type {ComponentProps} from "react";
import {useMemo} from "react";
import ReactMarkdown, {type Components} from "react-markdown";
import remarkGfm from "remark-gfm";
import {FileText, Sparkles, X} from "lucide-react";
import type {AgentEvent} from "./useAgentStream";
import {MessagePill} from "./AgentChat";

import PersonCard from "./cards/PersonCard";
import ProjectCard from "./cards/ProjectCard";
import ConceptCard from "./cards/ConceptCard";

interface ToolCallSnapshot {
    name: string;
    args: unknown;
    result?: unknown;
}

interface SidePanelProps {
    liveEvents: AgentEvent[];
    completedAnswerText?: string;
    completedToolCalls?: ToolCallSnapshot[];
    isRunning?: boolean;
    onClose?: () => void;
    userQuery?: string;
}

type ActiveEntity =
    | { type: "person"; data: ComponentProps<typeof PersonCard>["data"] }
    | { type: "project"; data: ComponentProps<typeof ProjectCard>["data"] }
    | { type: "concept"; data: ComponentProps<typeof ConceptCard>["data"] };

export default function SidePanel({
                                      liveEvents,
                                      completedAnswerText = "",
                                      completedToolCalls = [],
                                      isRunning = false,
                                      onClose,
                                      userQuery,
                                  }: SidePanelProps) {
    const active = useMemo(
        () => findLatestEntity(liveEvents, completedToolCalls),
        [completedToolCalls, liveEvents],
    );

    const showEntityCard = useMemo(() => {
        if (!active) return false;
        if (isRunning) return true;
        return isQueryAboutEntity(userQuery, active);
    }, [active, isRunning, userQuery]);

    const finalAnswer = !isRunning ? completedAnswerText.trim() : "";

    const entityCardToRender = active ? (
        <div>
            {active.type === "person" && <PersonCard data={active.data}/>}
            {active.type === "project" && <ProjectCard data={active.data}/>}
            {active.type === "concept" && <ConceptCard data={active.data}/>}
        </div>
    ) : null;

    return (
        <section
            className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-outline-variant/50 bg-surface-container-lowest shadow-sm">
            <header
                className="flex items-center justify-between border-b border-outline-variant/40 bg-surface-container-low px-4 py-3">
                <div className="min-w-0 flex items-center gap-2">
                    <Sparkles className="h-4 w-4 text-primary shrink-0 animate-pulse"/>
                    <div className="min-w-0">
                        <div className="text-label-caps font-label-caps text-on-surface-variant">Memento</div>
                        <div className="mt-0.5 text-sm font-semibold text-on-surface">Dimensional Memory</div>
                    </div>
                </div>
                {onClose && (
                    <button
                        onClick={onClose}
                        className="rounded p-1 text-on-surface-variant/80 hover:bg-surface-container-high transition"
                        aria-label="Close panel"
                    >
                        <X className="h-4 w-4"/>
                    </button>
                )}
            </header>

            <div className="flex-1 overflow-y-auto p-5">
                {showEntityCard && entityCardToRender ? (
                    entityCardToRender
                ) : finalAnswer ? (
                    <FinalAnswerPanel text={finalAnswer}/>
                ) : entityCardToRender ? (
                    entityCardToRender
                ) : (
                    <div
                        className="flex h-full flex-col items-center justify-center text-center p-6 text-on-surface-variant">
                        <div
                            className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-primary/5 text-primary border border-primary/10">
                            <Sparkles className="h-6 w-6"/>
                        </div>
                        <h3 className="text-sm font-semibold text-on-surface mb-2">
                            Context Panel Ready
                        </h3>
                        <p className="max-w-[280px] text-xs leading-5 text-on-surface-variant/85">
                            Ask about a contact, project name, or core topic. Relevant timeline data and synthesized
                            summaries will automatically materialize here.
                        </p>
                    </div>
                )}
            </div>
        </section>
    );
}

function isQueryAboutEntity(query: string | undefined, entity: ActiveEntity): boolean {
    if (!query) return false;
    const qLower = query.toLowerCase();

    // Helper to extract significant terms (filtering out common small/generic words)
    const getSignificantTerms = (str: string): string[] => {
        return str
            .toLowerCase()
            .split(/[\s,.\-_@]+/)
            .map((t) => t.trim())
            .filter((t) => t.length > 2 && !["the", "and", "for", "with", "from", "about", "what", "who", "when", "where", "how", "this", "that", "was", "were"].includes(t));
    };

    const entityTerms: string[] = [];
    const exactMatches: string[] = [];

    if (entity.type === "person") {
        if (entity.data.canonical_name) {
            entityTerms.push(...getSignificantTerms(entity.data.canonical_name));
            exactMatches.push(entity.data.canonical_name.toLowerCase());
        }
        if (entity.data.primary_email) {
            entityTerms.push(...getSignificantTerms(entity.data.primary_email));
            exactMatches.push(entity.data.primary_email.toLowerCase());
        }
        if (entity.data.aliases) {
            for (const alias of entity.data.aliases) {
                if (alias.display_name) {
                    entityTerms.push(...getSignificantTerms(alias.display_name));
                    exactMatches.push(alias.display_name.toLowerCase());
                }
                if (alias.email_address) {
                    entityTerms.push(...getSignificantTerms(alias.email_address));
                    exactMatches.push(alias.email_address.toLowerCase());
                }
            }
        }
    } else if (entity.type === "project") {
        if (entity.data.name) {
            entityTerms.push(...getSignificantTerms(entity.data.name));
            exactMatches.push(entity.data.name.toLowerCase());
        }
        if (entity.data.slug) {
            entityTerms.push(...getSignificantTerms(entity.data.slug));
            exactMatches.push(entity.data.slug.toLowerCase());
        }
    } else if (entity.type === "concept") {
        if (entity.data.name) {
            entityTerms.push(...getSignificantTerms(entity.data.name));
            exactMatches.push(entity.data.name.toLowerCase());
        }
        if (entity.data.slug) {
            entityTerms.push(...getSignificantTerms(entity.data.slug));
            exactMatches.push(entity.data.slug.toLowerCase());
        }
        if (entity.data.seed_keywords) {
            for (const kw of entity.data.seed_keywords) {
                entityTerms.push(...getSignificantTerms(kw));
                exactMatches.push(kw.toLowerCase());
            }
        }
    }

    // 1. Check if the query contains any exact match of full names/emails/slugs
    for (const match of exactMatches) {
        if (match.length > 2 && qLower.includes(match)) {
            return true;
        }
    }

    // 2. Check if a significant term of the entity's name/aliases is present in the query
    const uniqueEntityTerms = Array.from(new Set(entityTerms)).filter((t) => t.length > 2);
    for (const term of uniqueEntityTerms) {
        const regex = new RegExp(`\\b${term}\\b`, "i");
        if (regex.test(qLower)) {
            return true;
        }
    }

    return false;
}

function findLatestEntity(
    liveEvents: AgentEvent[],
    completedToolCalls: ToolCallSnapshot[],
): ActiveEntity | null {
    for (let i = liveEvents.length - 1; i >= 0; i--) {
        const event = liveEvents[i];
        if (event.type !== "tool_call_result" || !event.result) continue;
        const entity = entityFromToolResult(event.name, event.result);
        if (entity) return entity;
    }

    for (let i = completedToolCalls.length - 1; i >= 0; i--) {
        const call = completedToolCalls[i];
        if (call.result === undefined) continue;
        const entity = entityFromToolResult(call.name, call.result);
        if (entity) return entity;
    }

    return null;
}

function entityFromToolResult(name: string, result: unknown): ActiveEntity | null {
    if (name === "get_person_summary") {
        return {type: "person", data: result as ComponentProps<typeof PersonCard>["data"]};
    }
    if (name === "get_project_summary") {
        return {type: "project", data: result as ComponentProps<typeof ProjectCard>["data"]};
    }
    if (name === "get_concept_summary") {
        return {type: "concept", data: result as ComponentProps<typeof ConceptCard>["data"]};
    }
    return null;
}

function FinalAnswerPanel({text}: { text: string }) {
    const textWithCitationLinks = text.replace(/\[msg:(\d+)\](?!\()/g, "[msg:$1](#msg-$1)");

    return (
        <div className="space-y-4">
            <div className="rounded-lg border border-outline-variant/40 bg-surface-container-low p-4">
                <div className="flex items-start gap-3">
                    <div
                        className="flex h-9 w-9 shrink-0 items-center justify-center rounded bg-primary-fixed text-on-primary-fixed-variant">
                        <FileText className="h-4 w-4"/>
                    </div>
                    <div className="min-w-0">
                        <h3 className="text-sm font-semibold text-on-surface">Final Answer</h3>
                        <p className="mt-1 text-xs leading-5 text-on-surface-variant">
                            Synthesized from the completed Ask Memento run.
                        </p>
                    </div>
                </div>
            </div>

            <article
                className="rounded-lg border border-outline-variant/45 bg-background p-4 text-on-surface shadow-xs">
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents} disallowedElements={["img"]}>
                    {textWithCitationLinks}
                </ReactMarkdown>
            </article>
        </div>
    );
}

const markdownComponents: Components = {
    p: ({children}) => <p className="text-sm leading-6 mb-3 last:mb-0">{children}</p>,
    ul: ({children}) => <ul className="list-disc pl-5 space-y-1.5 my-2 text-sm leading-6">{children}</ul>,
    ol: ({children}) => <ol className="list-decimal pl-5 space-y-1.5 my-2 text-sm leading-6">{children}</ol>,
    li: ({children}) => <li className="text-sm leading-6">{children}</li>,
    h1: ({children}) => <h1 className="mt-3 mb-2 text-base font-bold text-on-surface first:mt-0">{children}</h1>,
    h2: ({children}) => <h2 className="mt-3 mb-2 text-base font-bold text-on-surface first:mt-0">{children}</h2>,
    h3: ({children}) => <h3 className="mt-3 mb-1.5 text-sm font-semibold text-on-surface first:mt-0">{children}</h3>,
    strong: ({children}) => <strong className="font-semibold text-on-surface">{children}</strong>,
    code: ({children}) => (
        <code className="rounded bg-surface-container px-1 py-0.5 font-mono text-[0.85em] text-on-surface">
            {children}
        </code>
    ),
    table: ({children}) => (
        <div className="my-3 overflow-x-auto rounded border border-outline-variant/35">
            <table className="w-full border-collapse text-sm">{children}</table>
        </div>
    ),
    th: ({children}) => (
        <th className="border-b border-outline-variant/35 bg-surface-container-low px-2.5 py-2 text-left text-xs font-semibold text-on-surface">
            {children}
        </th>
    ),
    td: ({children}) => (
        <td className="border-t border-outline-variant/25 px-2.5 py-2 align-top text-sm leading-5">{children}</td>
    ),
    a: ({href, children}) => {
        if (!href) {
            return <>{children}</>;
        }
        if (href.startsWith("#msg-")) {
            const messageId = Number.parseInt(href.slice(5), 10);
            if (Number.isFinite(messageId) && messageId > 0) {
                return <MessagePill messageId={messageId}/>;
            }
        }
        if (href.startsWith("/")) {
            return (
                <a href={href} className="font-semibold text-primary hover:underline">
                    {children}
                </a>
            );
        }
        return (
            <a
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                className="font-semibold text-primary hover:underline"
            >
                {children}
            </a>
        );
    },
};
