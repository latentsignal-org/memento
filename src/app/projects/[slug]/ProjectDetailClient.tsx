"use client";

import type {KeyboardEvent, MouseEvent} from "react";
import {useMemo, useRef, useState} from "react";
import Link from "next/link";
import {useRouter} from "next/navigation";
import {revalidateEntityPath} from "@/app/actions";
import {contactInitials, displayContactName, maskEmailAddresses} from "@/lib/contact-display";
import {slugify} from "@/lib/citation";
import ProjectCompileButton from "@/components/agent/ProjectCompileButton";
import HowThisWasBuiltPanel from "@/components/agent/HowThisWasBuiltPanel";
import {formatMonthDay} from "@/lib/date-utils";
import {buildCitationIndexMap, EvidenceText,} from "@/components/evidence/EvidenceCitations";
import {formatEvidenceLabel} from "@/components/evidence/labels";
import MessagePreviewPanel from "@/components/evidence/MessagePreviewPanel";
import MessageRow from "@/components/evidence/MessageRow";
import {bestPreviewExcerpt, normalizePreviewText} from "@/components/evidence/message-utils";
import {useMessageDetail} from "@/components/evidence/useMessageDetail";

// Interfaces matching the data structure
export interface ProjectMember {
    person_id: number;
    canonical_name: string;
    primary_email: string;
    avatar_url?: string;
    role: string;
    slug: string;
}

export interface ProjectTimelineItem {
    message_id: number;
    date: string;
    subject: string;
    from_canonical_name: string;
    from_email: string;
    direction: "to_account" | "from_account" | "other";
    snippet: string;
    body_text?: string;
}

export interface ProjectPhase {
    title: string;
    date_range: string;
    content: string;
    source_message_ids: number[];
}

export interface ProjectFrictionPoint {
    text: string;
    source_message_ids: number[];
}

export interface ProjectNarrative {
    summary: string;
    phases: ProjectPhase[];
    friction_points: ProjectFrictionPoint[];
    current_understanding: string;
}

export interface ProjectDecision {
    title: string;
    value: string;
}

export interface ProjectExpense {
    category: string;
    amount: string;
}

export interface ProjectAttachment {
    name: string;
}

export interface ProjectData {
    project_id: number;
    slug: string;
    name: string;
    aliases: string[];
    status: string;
    started_at: string;
    updated_at: string;
    members: ProjectMember[];
    message_count: number;
    date_range: {
        first: string;
        last: string;
    };
    timeline: ProjectTimelineItem[];
    narrative: ProjectNarrative;
    decisions?: ProjectDecision[];
    expenses?: ProjectExpense[];
    attachments?: ProjectAttachment[];
}

interface ProjectDetailClientProps {
    project: ProjectData;
    simulationMode?: boolean;
    simulationDelayMs?: number | null;
}

const EMPTY_NARRATIVE: ProjectNarrative = {
    summary: "",
    phases: [],
    friction_points: [],
    current_understanding: "",
};

export default function ProjectDetailClient({
                                                project,
                                                simulationMode = false,
                                                simulationDelayMs = null,
                                            }: ProjectDetailClientProps) {
    const router = useRouter();
    const [isEditingTitle, setIsEditingTitle] = useState(false);
    const [titleDraft, setTitleDraft] = useState(project.name);
    const [titleValue, setTitleValue] = useState(project.name);
    const [titleError, setTitleError] = useState<string | null>(null);
    const narrative: ProjectNarrative = {
        ...EMPTY_NARRATIVE,
        ...(project.narrative || {}),
        phases: Array.isArray(project.narrative?.phases) ? project.narrative.phases : [],
        friction_points: Array.isArray(project.narrative?.friction_points) ? project.narrative.friction_points : [],
    };
    const hasGeneratedNarrative =
        !!narrative.summary ||
        narrative.phases.length > 0 ||
        narrative.friction_points.length > 0 ||
        !!narrative.current_understanding;

    const [isGenerating, setIsGenerating] = useState(false);

    // Search & Filtering States
    const [searchQuery, setSearchQuery] = useState("");
    const [directionFilter, setDirectionFilter] = useState<"all" | "inbound" | "outbound">("all");
    const [selectedMemberFilter, setSelectedMemberFilter] = useState<string | null>(null);

    // Interaction States
    const [hoveredMsgId, setHoveredMsgId] = useState<number | null>(null);
    const [tooltipPos, setTooltipPos] = useState<{ top: number; left: number } | null>(null);
    const [highlightedMsgId, setHighlightedMsgId] = useState<number | null>(null);
    const [selectedMessageId, setSelectedMessageId] = useState<number | null>(
        project.timeline.find((message) => normalizePreviewText(message.body_text || message.snippet))?.message_id ??
        project.timeline[0]?.message_id ??
        null,
    );
    const [isStreamCollapsed, setIsStreamCollapsed] = useState(false);
    const tooltipHideTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

    // Message Map for instant lookups
    const messageMap = useMemo(() => {
        const map = new Map<number, ProjectTimelineItem>();
        project.timeline.forEach((msg) => {
            map.set(msg.message_id, msg);
        });
        return map;
    }, [project.timeline]);
    const selectedMessage = selectedMessageId ? messageMap.get(selectedMessageId) ?? null : null;
    const {detail: selectedMessageDetail, isLoading: selectedMessageLoading, error: selectedMessageError} =
        useMessageDetail(selectedMessageId);

    // Unique senders extracted from timeline for filtering pills
    const uniqueSenders = useMemo(() => {
        const senders = new Set<string>();
        project.timeline.forEach((msg) => {
            if (msg.from_canonical_name) {
                senders.add(msg.from_canonical_name);
            }
        });
        return Array.from(senders).sort();
    }, [project.timeline]);


    // Scroll and highlight a message
    const handleCitationClick = (msgId: number) => {
        setSelectedMessageId(msgId);
    };

    const locateMessageInTimeline = (msgId: number) => {
        setIsStreamCollapsed(false);
        setHighlightedMsgId(msgId);

        setTimeout(() => {
            const element = document.getElementById(`msg-card-${msgId}`);
            if (element) {
                const rect = element.getBoundingClientRect();
                const absoluteTop = window.scrollY + rect.top;
                const topOffset = Math.max(120, window.innerHeight * 0.18);
                window.scrollTo({
                    top: Math.max(0, absoluteTop - topOffset),
                    behavior: "smooth",
                });
            }
        }, 100);

        setTimeout(() => {
            setHighlightedMsgId((current) => (current === msgId ? null : current));
        }, 3000);
    };

    const clearTooltipHideTimer = () => {
        if (tooltipHideTimer.current) {
            clearTimeout(tooltipHideTimer.current);
            tooltipHideTimer.current = null;
        }
    };

    const hideCitationTooltip = () => {
        clearTooltipHideTimer();
        setHoveredMsgId(null);
        setTooltipPos(null);
    };

    const scheduleCitationTooltipHide = () => {
        clearTooltipHideTimer();
        tooltipHideTimer.current = setTimeout(hideCitationTooltip, 180);
    };

    // Hover Tooltip Position & Data
    const handleCitationHover = (
        msgId: number | null,
        e: MouseEvent<HTMLButtonElement> | null
    ) => {
        if (msgId === null || !e) {
            scheduleCitationTooltipHide();
            return;
        }
        clearTooltipHideTimer();
        const rect = e.currentTarget.getBoundingClientRect();
        setHoveredMsgId(msgId);
        setTooltipPos({
            top: rect.top - 8,
            left: rect.left + rect.width / 2,
        });
    };

    const handleTooltipOpenMessage = () => {
        if (!hoveredMessage) return;
        handleCitationClick(hoveredMessage.message_id);
        hideCitationTooltip();
    };

    const handleTooltipKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            handleTooltipOpenMessage();
        }
        if (event.key === "Escape") {
            hideCitationTooltip();
        }
    };

    // Dynamic Citation Index Mapping
    const citationMap = useMemo(
        () =>
            buildCitationIndexMap([
                narrative.summary,
                ...narrative.phases.map((phase) => phase.content),
                ...narrative.friction_points.map((point) => point.text),
                narrative.current_understanding,
            ]),
        [narrative],
    );

    // Group Messages into Phases based on dates or explicit references
    const groupedPhases = useMemo(() => {
        const phases = narrative.phases.map((phase) => {
            const [startStr, endStr] = phase.date_range.split(" to ");
            return {
                ...phase,
                startDate: startStr || "",
                endDate: endStr || "",
                messages: [] as ProjectTimelineItem[],
            };
        });

        const uncategorized: ProjectTimelineItem[] = [];

        // Filtered timeline based on filters (search, direction, member)
        const filteredTimeline = project.timeline.filter((msg) => {
            // 1. Search Query
            if (searchQuery.trim() !== "") {
                const q = searchQuery.toLowerCase();
                const matchesSubject = msg.subject?.toLowerCase().includes(q);
                const matchesSnippet = msg.snippet?.toLowerCase().includes(q);
                const matchesSender = msg.from_canonical_name?.toLowerCase().includes(q);
                const matchesId = String(msg.message_id) === q;
                if (!matchesSubject && !matchesSnippet && !matchesSender && !matchesId) {
                    return false;
                }
            }

            // 2. Direction
            if (directionFilter === "inbound" && msg.direction !== "to_account") return false;
            if (directionFilter === "outbound" && msg.direction !== "from_account") return false;

            // 3. Member
            if (selectedMemberFilter) {
                if (msg.from_canonical_name !== selectedMemberFilter) return false;
            }

            return true;
        });

        // Place filtered messages into matching phases
        filteredTimeline.forEach((msg) => {
            const msgDate = msg.date;
            let matched = false;

            phases.forEach((phase) => {
                // Match if explicitly listed as source ID or falls inside date range
                const isSource = phase.source_message_ids?.includes(msg.message_id);
                const inRange = msgDate >= phase.startDate && msgDate <= phase.endDate;

                if (isSource || inRange) {
                    phase.messages.push(msg);
                    matched = true;
                }
            });

            if (!matched) {
                uncategorized.push(msg);
            }
        });

        // Sort chronologically (ascending date)
        phases.forEach((phase) => {
            phase.messages.sort((a, b) => a.date.localeCompare(b.date));
        });
        uncategorized.sort((a, b) => a.date.localeCompare(b.date));

        return {phases, uncategorized};
    }, [project.timeline, narrative.phases, searchQuery, directionFilter, selectedMemberFilter]);
    const hasStructuredNarrative = narrative.phases.length > 0;

    const hoveredMessage = hoveredMsgId ? messageMap.get(hoveredMsgId) : null;
    // Pastel colors generator helper
    const getPastelColor = (idx: number) => {
        const colors = [
            "bg-[#c4ebde] text-[#00201a]", // Soft mint green
            "bg-[#d5e5ee] text-[#0f1d24]", // Soft blue
            "bg-[#ffdad4] text-[#30130e]", // Soft pink
            "bg-[#efeeec] text-[#414846]", // Soft gray
        ];
        return colors[idx % colors.length];
    };

    // Roman numeral converter helper
    const getRomanNumeral = (num: number) => {
        const map = ["I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"];
        return map[num] || String(num + 1);
    };

    const renderTimelineRow = (item: ProjectTimelineItem) => {
        const isOutbound = item.direction === "from_account";
        const isSelected = selectedMessageId === item.message_id;
        const isHighlighted = highlightedMsgId === item.message_id;
        const directionLabel = isOutbound ? "Outbound" : "Inbound";

        return (
            <div key={item.message_id} className="relative group">
        <span
            className={`absolute -left-[31px] top-5 h-3 w-3 rounded-full ring-4 ring-white transition-colors duration-200 z-10 ${
                isSelected
                    ? "bg-primary"
                    : isHighlighted
                        ? "bg-primary"
                        : "bg-outline-variant group-hover:bg-outline"
            }`}
        />
                <MessageRow
                    id={`msg-card-${item.message_id}`}
                    messageId={item.message_id}
                    subject={item.subject}
                    snippet={bestPreviewExcerpt(item.subject, item.snippet, item.body_text)}
                    dateLabel={formatMonthDay(item.date)}
                    selected={isSelected}
                    highlighted={isHighlighted}
                    badge={{
                        label: directionLabel,
                        tone: isOutbound ? "outbound" : "inbound",
                    }}
                    metadata={
                        <div className="space-y-0.5 mb-2">
                            <p className="text-[11px] text-on-surface-variant">
                                From: <span
                                className="font-semibold text-on-surface">{maskEmailAddresses(item.from_canonical_name)}</span>
                            </p>
                            {item.from_email ? (
                                <p className="text-[11px] font-mono text-on-surface-variant/85">
                                    {maskEmailAddresses(item.from_email)}
                                </p>
                            ) : null}
                        </div>
                    }
                    onSelect={() => handleCitationClick(item.message_id)}
                />
            </div>
        );
    };

    const saveTitle = async () => {
        const name = titleDraft.trim();
        if (!name || name === titleValue) {
            setIsEditingTitle(false);
            setTitleDraft(titleValue);
            return;
        }
        setTitleError(null);
        const res = await fetch(`/api/projects/${project.slug}`, {
            method: "PATCH",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({name}),
        });
        if (!res.ok) {
            setTitleError("Could not save title. Make sure the Go server is restarted.");
            return;
        }
        setTitleValue(name);
        setIsEditingTitle(false);
        void revalidateEntityPath(`/projects/${project.slug}`)
            .catch((error) => console.error("revalidate project path", error))
            .finally(() => {
                window.location.reload();
            });
    };

    return (
        <div className="max-w-[1280px] mx-auto px-6 py-8 relative">

            {/* Back link */}
            <div className="mb-6 flex justify-between items-center">
                <Link
                    href="/projects"
                    className="inline-flex items-center gap-1.5 text-ui-small font-bold text-on-surface-variant hover:text-primary transition-colors"
                >
                    <span className="material-symbols-outlined text-sm">arrow_back</span>
                    Back to Projects
                </Link>
            </div>
            {simulationMode && (
                <div
                    className="mb-6 rounded-lg border border-amber-300/80 bg-amber-50 px-4 py-3 text-[12px] font-semibold text-amber-900">
                    Simulation mode: generation runs in harness mode (no LLM token usage).
                </div>
            )}

            {/* Hero / Header Section */}
            <header className="mb-10 pb-8 border-b border-outline-variant/40">
                <div className="flex flex-col lg:flex-row lg:items-end lg:justify-between gap-6">
                    {/* Left: pills → title → members */}
                    <div className="space-y-3 flex-1 min-w-0">
                        <div className="flex flex-wrap gap-2">
              <span
                  className={`px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider ${
                      simulationMode ? "bg-amber-600 text-white" : "bg-primary text-white"
                  }`}
              >
                Project
              </span>
                            {simulationMode && (
                                <span
                                    className="bg-amber-100 text-amber-900 border border-amber-300/80 px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">
                  Sim
                </span>
                            )}
                            {project.status !== "active" && (
                                <span
                                    className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">
                  {project.status}
                </span>
                            )}
                            <span
                                className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold">
                {project.message_count} messages
              </span>
                        </div>
                        {isEditingTitle ? (
                            <div className="space-y-1">
                                <div className="flex w-full max-w-5xl min-w-0 items-center gap-3">
                                    <input
                                        autoFocus
                                        value={titleDraft}
                                        onChange={(e) => setTitleDraft(e.target.value)}
                                        className="min-w-0 flex-1 rounded border border-outline-variant px-3 py-2 text-headline-md"
                                    />
                                    <div className="flex items-center gap-2 whitespace-nowrap">
                                        <button type="button" onClick={() => void saveTitle()}
                                                className="text-ui-small font-bold text-primary">Save
                                        </button>
                                        <button type="button" onClick={() => {
                                            setIsEditingTitle(false);
                                            setTitleDraft(titleValue);
                                        }} className="text-ui-small text-on-surface-variant">Cancel
                                        </button>
                                    </div>
                                </div>
                                {titleError ? <p className="text-[12px] text-error">{titleError}</p> : null}
                            </div>
                        ) : (
                            <h1 className="text-display-lg font-display-lg text-primary tracking-tight leading-tight flex items-center gap-2">
                                <span>{titleValue}</span>
                                <button type="button" onClick={() => setIsEditingTitle(true)}
                                        className="text-on-surface-variant hover:text-primary">
                                    <span className="material-symbols-outlined text-base">edit</span>
                                </button>
                            </h1>
                        )}
                        <button
                            className="text-ui-medium text-on-surface-variant font-mono text-left hover:text-primary hover:underline transition-colors cursor-pointer"
                            onClick={() => document.getElementById("the-cast")?.scrollIntoView({
                                behavior: "smooth",
                                block: "start"
                            })}
                        >
                            {project.members.length > 0
                                ? project.members
                                    .slice(0, 3)
                                    .map((m) => displayContactName(m.canonical_name, m.primary_email))
                                    .join(" · ") +
                                (project.members.length > 3 ? ` · +${project.members.length - 3} more` : "")
                                : `${project.message_count} messages`}
                        </button>
                        {project.aliases.length > 0 && (
                            <p className="text-[11px] text-on-surface-variant font-mono opacity-70">
                                Aliases: {project.aliases.join(" · ")}
                            </p>
                        )}
                    </div>

                    {/* Right: date range → updated + version history */}
                    <div className="text-left lg:text-right text-ui-small text-on-surface-variant">
                        {(project.date_range.first || project.date_range.last) && (
                            <p>{formatMonthDay(project.date_range.first)} – {formatMonthDay(project.date_range.last)}</p>
                        )}
                        {project.updated_at && (
                            <div className="mt-1">
                                <span>Updated {formatMonthDay(project.updated_at)}</span>
                            </div>
                        )}
                    </div>
                </div>
            </header>

            {/* Main Content Grid */}
            <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-start">

                {/* Left Side: Summary & Deep Narrative Phases (8 cols) */}
                <section className="lg:col-span-8 space-y-10">

                    {/* Executive Summary */}
                    <div
                        className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 sm:p-8 shadow-sm space-y-4">
                        <div
                            className="grid grid-cols-1 gap-4 border-b border-outline-variant/40 pb-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-start">
                            <div>
                                <h2 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px]">
                                    EXECUTIVE SUMMARY
                                </h2>
                                {!isGenerating && (
                                    <p className="mt-2 text-ui-small text-on-surface-variant">
                                        Create a concise project brief from attached messages and citations.
                                    </p>
                                )}
                            </div>
                            <div className="contents">
                                <ProjectCompileButton
                                    slug={project.slug}
                                    hasGenerated={hasGeneratedNarrative}
                                    onRunningChange={setIsGenerating}
                                    cardLayout="full-row"
                                    simulateByDefault={simulationMode}
                                    simulationDelayMs={simulationDelayMs ?? undefined}
                                />
                                {hasGeneratedNarrative && !isGenerating ? (
                                    <div
                                        className="flex flex-wrap items-center justify-end gap-4 md:col-start-2 md:justify-self-end">
                                        <HowThisWasBuiltPanel
                                            sessionType="project_compile"
                                            entityId={project.slug}
                                            provenanceDimension="projects"
                                            provenanceSlug={project.slug}
                                            buttonStyle="link"
                                        />
                                    </div>
                                ) : null}
                            </div>
                        </div>
                        <div className="text-body-reading font-body-reading text-on-surface leading-relaxed font-serif">
                            {narrative.summary ? (
                                <EvidenceText
                                    text={narrative.summary}
                                    citationIndexMap={citationMap}
                                    onSelect={handleCitationClick}
                                    onHover={handleCitationHover}
                                />
                            ) : (
                                <div className="space-y-4">
                                    <p className="text-on-surface-variant italic">
                                        No project brief yet. Generate one from the attached messages.
                                    </p>
                                </div>
                            )}
                        </div>
                    </div>

                    {/* Narrative Phases */}
                    {narrative.phases.length > 0 && (
                        <div className="space-y-8">
                            {narrative.phases.map((phase, idx) => (
                                <div key={idx}
                                     className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 sm:p-8 shadow-sm space-y-4">
                                    <div
                                        className="flex justify-between items-center border-b border-outline-variant/40 pb-2 flex-wrap gap-2">
                                        <h3 className="text-headline-md font-headline-md text-primary font-bold">
                                            Phase {getRomanNumeral(idx)}: {phase.title}
                                        </h3>
                                        <span
                                            className="text-[11px] font-mono text-on-surface-variant bg-surface-container-high/60 px-2 py-0.5 rounded font-bold">
                      {phase.date_range}
                    </span>
                                    </div>
                                    <div
                                        className="text-body-reading font-body-reading text-on-surface leading-relaxed font-serif">
                                        <EvidenceText
                                            text={phase.content}
                                            citationIndexMap={citationMap}
                                            onSelect={handleCitationClick}
                                            onHover={handleCitationHover}
                                        />
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}

                    {/* Current Understanding */}
                    {narrative.current_understanding && (
                        <div
                            className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 sm:p-8 shadow-sm space-y-4">
                            <h2 className="text-label-caps text-primary font-bold border-b border-outline-variant/40 pb-2">
                                CURRENT STATUS & UNDERSTANDING
                            </h2>
                            <div
                                className="text-body-reading font-body-reading text-on-surface leading-relaxed font-serif">
                                <EvidenceText
                                    text={narrative.current_understanding}
                                    citationIndexMap={citationMap}
                                    onSelect={handleCitationClick}
                                    onHover={handleCitationHover}
                                />
                            </div>
                        </div>
                    )}

                    {/* Friction Points & Delays */}
                    {narrative.friction_points.length > 0 && (
                        <div
                            className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 sm:p-8 shadow-sm space-y-4">
                            <h2 className="text-label-caps text-primary font-bold border-b border-outline-variant/40 pb-2">
                                FRICTION POINTS & DELAYS
                            </h2>
                            <div className="space-y-4">
                                {narrative.friction_points.map((pt, idx) => (
                                    <div
                                        key={idx}
                                        className="p-4 bg-white border border-outline-variant/40 rounded-xl hover:border-outline-variant transition-colors shadow-sm"
                                    >
                                        <div className="flex gap-3 items-start">
                      <span className="material-symbols-outlined text-destructive text-[20px] flex-shrink-0 mt-0.5">
                        warning
                      </span>
                                            <div
                                                className="text-ui-medium font-ui-medium text-on-surface leading-relaxed">
                                                <EvidenceText
                                                    text={pt.text}
                                                    citationIndexMap={citationMap}
                                                    onSelect={handleCitationClick}
                                                    onHover={handleCitationHover}
                                                />
                                            </div>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}

                    <section id="correspondence-stream"
                             className="border border-outline-variant/40 rounded-2xl bg-surface-container-low overflow-hidden shadow-sm">
                        <button
                            onClick={() => setIsStreamCollapsed(!isStreamCollapsed)}
                            className="w-full flex justify-between items-center px-6 py-5 bg-surface-container hover:bg-surface-container-high transition-colors text-left"
                        >
                            <div className="flex items-center gap-2.5">
                <span className="material-symbols-outlined text-primary text-xl">
                  mail
                </span>
                                <span className="text-ui-medium font-bold text-primary font-ui-medium">
                  Supporting Emails ({project.timeline.length})
                </span>
                            </div>
                            <div className="flex items-center gap-2">
                <span
                    className="text-label-caps text-on-surface-variant bg-white px-2 py-0.5 rounded border border-outline-variant/30 font-bold font-mono">
                  MESSAGE ARCHIVE
                </span>
                                <span
                                    className={`material-symbols-outlined text-on-surface-variant transition-transform duration-200 ${isStreamCollapsed ? "" : "rotate-180"}`}>
                  expand_more
                </span>
                            </div>
                        </button>

                        {!isStreamCollapsed && (
                            <div className="p-6 space-y-6 border-t border-outline-variant/40">
                                <div className="space-y-4">
                                    <div
                                        className="flex flex-col sm:flex-row gap-3 justify-between items-stretch sm:items-center bg-white/60 p-4 rounded-xl border border-outline-variant/30">
                                        <div className="relative flex-1">
                                            <input
                                                type="text"
                                                placeholder="Search supporting emails..."
                                                value={searchQuery}
                                                onChange={(e) => setSearchQuery(e.target.value)}
                                                className="w-full bg-white border border-outline-variant focus:ring-1 focus:ring-primary rounded-xl text-ui-small px-3 py-2 pl-9 pr-8 focus:outline-none transition-all placeholder-on-surface-variant/50"
                                            />
                                            <span
                                                className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-on-surface-variant text-[18px] pointer-events-none">
                        search
                      </span>
                                            {searchQuery && (
                                                <button
                                                    onClick={() => setSearchQuery("")}
                                                    className="absolute right-3 top-1/2 -translate-y-1/2 text-on-surface-variant hover:text-primary text-[16px] cursor-pointer"
                                                >
                                                    <span className="material-symbols-outlined">close</span>
                                                </button>
                                            )}
                                        </div>

                                        <div
                                            className="flex border border-outline-variant rounded-xl overflow-hidden bg-white">
                                            {(["all", "inbound", "outbound"] as const).map((dir) => (
                                                <button
                                                    key={dir}
                                                    onClick={() => setDirectionFilter(dir)}
                                                    className={`px-3 py-2 text-ui-small font-bold capitalize transition-colors cursor-pointer ${
                                                        directionFilter === dir
                                                            ? "bg-primary text-on-primary"
                                                            : "text-on-surface-variant hover:bg-surface-container-low"
                                                    }`}
                                                >
                                                    {dir === "all" ? "All" : dir}
                                                </button>
                                            ))}
                                        </div>
                                    </div>

                                    <div className="flex items-center gap-2 flex-wrap">
                    <span
                        className="text-[10px] text-on-surface-variant font-bold uppercase tracking-wider select-none mr-1">
                      Filter by Sender:
                    </span>
                                        <button
                                            onClick={() => setSelectedMemberFilter(null)}
                                            className={`px-2.5 py-1 rounded-full text-[11px] font-bold border transition-all duration-150 cursor-pointer ${
                                                selectedMemberFilter === null
                                                    ? "bg-primary text-white border-primary shadow-sm"
                                                    : "bg-white text-on-surface-variant border-outline-variant/50 hover:bg-surface-container-low"
                                            }`}
                                        >
                                            All Senders
                                        </button>
                                        {uniqueSenders.map((sender) => (
                                            <button
                                                key={sender}
                                                onClick={() => setSelectedMemberFilter(sender)}
                                                className={`px-2.5 py-1 rounded-full text-[11px] font-bold border transition-all duration-150 cursor-pointer ${
                                                    selectedMemberFilter === sender
                                                        ? "bg-[#c4ebde] text-[#00201a] border-[#12362e]/30 shadow-sm"
                                                        : "bg-white text-on-surface-variant border-outline-variant/50 hover:bg-surface-container-low"
                                                }`}
                                            >
                                                {maskEmailAddresses(sender)}
                                            </button>
                                        ))}
                                    </div>
                                </div>

                                {(searchQuery || directionFilter !== "all" || selectedMemberFilter) && (
                                    <div
                                        className="flex flex-wrap items-center gap-1.5 mt-2 bg-white/30 px-3 py-2 rounded-lg border border-outline-variant/20">
                                        <span className="text-[10px] text-on-surface-variant font-bold uppercase mr-1">Active Filters:</span>
                                        {searchQuery && (
                                            <span
                                                className="inline-flex items-center gap-1 bg-surface-container px-2 py-0.5 rounded text-[10px] text-on-surface-variant border border-outline-variant/30">
                        Query: &quot;{searchQuery}&quot;
                                                <button onClick={() => setSearchQuery("")}
                                                        className="hover:text-primary"><span
                                                    className="material-symbols-outlined text-[12px]">close</span></button>
                      </span>
                                        )}
                                        {directionFilter !== "all" && (
                                            <span
                                                className="inline-flex items-center gap-1 bg-surface-container px-2 py-0.5 rounded text-[10px] text-on-surface-variant border border-outline-variant/30">
                        Direction: {directionFilter}
                                                <button onClick={() => setDirectionFilter("all")}
                                                        className="hover:text-primary"><span
                                                    className="material-symbols-outlined text-[12px]">close</span></button>
                      </span>
                                        )}
                                        {selectedMemberFilter && (
                                            <span
                                                className="inline-flex items-center gap-1 bg-[#c4ebde] text-[#00201a] px-2 py-0.5 rounded text-[10px] font-bold border border-[#12362e]/10">
                        Author: {maskEmailAddresses(selectedMemberFilter)}
                                                <button onClick={() => setSelectedMemberFilter(null)}
                                                        className="hover:text-primary"><span
                                                    className="material-symbols-outlined text-[12px]">close</span></button>
                      </span>
                                        )}
                                        <button
                                            onClick={() => {
                                                setSearchQuery("");
                                                setDirectionFilter("all");
                                                setSelectedMemberFilter(null);
                                            }}
                                            className="text-[10px] text-primary hover:underline ml-auto font-bold"
                                        >
                                            Clear All
                                        </button>
                                    </div>
                                )}

                                <div className="space-y-8">
                                    {groupedPhases.phases.map((phase, pIdx) => {
                                        if (phase.messages.length === 0) return null;

                                        return (
                                            <div key={pIdx}
                                                 className="bg-white border border-outline-variant/40 rounded-2xl p-6 shadow-sm">
                                                <div className="mb-4">
                                                    <div
                                                        className="flex justify-between items-start gap-4 flex-wrap mb-1">
                                                        <h4 className="text-ui-medium font-bold text-primary flex items-center gap-2">
                              <span
                                  className="w-5 h-5 rounded bg-primary text-white flex items-center justify-center text-[10px] font-mono">
                                {pIdx + 1}
                              </span>
                                                            {phase.title}
                                                        </h4>
                                                        <span
                                                            className="text-[10px] font-mono text-on-surface-variant bg-surface-container px-2 py-0.5 rounded font-bold">
                              {phase.date_range}
                            </span>
                                                    </div>
                                                    <p className="text-ui-small text-on-surface-variant leading-relaxed">
                                                        {maskEmailAddresses(phase.content)}
                                                    </p>
                                                </div>

                                                <div
                                                    className="relative pl-6 py-2 space-y-4 border-l border-outline-variant/40 ml-2.5">
                                                    {phase.messages.map(renderTimelineRow)}
                                                </div>
                                            </div>
                                        );
                                    })}

                                    {groupedPhases.uncategorized.length > 0 && (
                                        <details
                                            open={!hasStructuredNarrative}
                                            className="group bg-white border border-outline-variant/40 rounded-2xl p-6 shadow-sm"
                                        >
                                            <summary
                                                className="flex justify-between items-center cursor-pointer select-none font-bold text-ui-medium text-on-surface-variant list-none">
                                                <div className="flex items-center gap-2">
                          <span
                              className="material-symbols-outlined text-on-surface-variant group-open:rotate-90 transition-transform">
                            chevron_right
                          </span>
                                                    <span>Additional Related Emails ({groupedPhases.uncategorized.length})</span>
                                                </div>
                                                <span
                                                    className="text-label-caps text-on-surface-variant bg-surface-container px-2 py-0.5 rounded font-mono font-bold">
                          BACKGROUND
                        </span>
                                            </summary>

                                            <div
                                                className="mt-6 pt-4 border-t border-outline-variant/40 relative pl-6 py-2 space-y-4 border-l border-outline-variant/40 ml-2.5">
                                                {groupedPhases.uncategorized.map(renderTimelineRow)}
                                            </div>
                                        </details>
                                    )}

                                    {groupedPhases.phases.every(p => p.messages.length === 0) && groupedPhases.uncategorized.length === 0 && (
                                        <div
                                            className="text-center py-16 bg-white border border-dashed border-outline-variant/60 rounded-2xl">
                      <span
                          className="material-symbols-outlined text-4xl text-on-surface-variant/40 mb-3 animate-pulse">
                        mail_lock
                      </span>
                                            <p className="text-ui-medium font-bold text-on-surface-variant">No
                                                supporting emails match your current filters.</p>
                                            <button
                                                onClick={() => {
                                                    setSearchQuery("");
                                                    setDirectionFilter("all");
                                                    setSelectedMemberFilter(null);
                                                }}
                                                className="mt-3 text-ui-small text-primary font-bold hover:underline"
                                            >
                                                Clear all filters
                                            </button>
                                        </div>
                                    )}
                                </div>
                            </div>
                        )}
                    </section>

                </section>

                {/* Right Side: Metadata Sidebar (4 cols) */}
                <aside
                    className="lg:col-span-4 space-y-6 lg:sticky lg:top-16 lg:max-h-[calc(100vh-4rem)] lg:overflow-y-auto min-w-0 lg:pb-8">
                    <div
                        className="bg-surface-container-low border border-outline-variant/40 rounded-2xl overflow-hidden shadow-sm">
                        <div className="px-6 py-5 border-b border-outline-variant/40 bg-surface-container">
                            <div className="flex items-start justify-between gap-4">
                                <div>
                                    <p className="text-[11px] uppercase tracking-[0.14em] text-on-surface-variant mb-2">Source</p>
                                    <h3 className="text-ui-medium font-bold text-primary">Supporting email</h3>
                                </div>
                                {selectedMessage && (
                                    <button
                                        type="button"
                                        onClick={() => locateMessageInTimeline(selectedMessage.message_id)}
                                        className="inline-flex items-center rounded-full bg-primary-fixed px-3 py-1 text-[11px] font-semibold text-on-primary-fixed hover:bg-primary-fixed-dim focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                                    >
                                        Locate in timeline
                                    </button>
                                )}
                            </div>
                        </div>

                        <MessagePreviewPanel
                            detail={selectedMessageDetail}
                            summary={
                                selectedMessage
                                    ? {
                                        messageId: selectedMessage.message_id,
                                        subject: selectedMessage.subject,
                                        snippet: selectedMessage.snippet,
                                        sentAt: selectedMessage.date,
                                        fromLabel: selectedMessage.from_canonical_name,
                                        fromEmail: "",
                                    }
                                    : null
                            }
                            isLoading={selectedMessageLoading}
                            error={selectedMessageError}
                            emptyText="Click any citation or timeline message to inspect the source email here without losing your place."
                            onLocate={
                                selectedMessage ? () => locateMessageInTimeline(selectedMessage.message_id) : null
                            }
                        />
                    </div>

                    {/* The Cast */}
                    {project.members.length > 0 && (
                        <div id="the-cast"
                             className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 space-y-4 shadow-sm scroll-mt-4">
                            <div
                                className="flex items-center gap-2 border-b border-outline-variant/40 pb-2 text-on-surface-variant">
                                <span className="material-symbols-outlined text-lg">group</span>
                                <h3 className="text-label-caps font-bold text-primary">THE CAST</h3>
                            </div>
                            <div className="space-y-2">
                                {project.members.map((member, idx) => {
                                    const isFiltered = selectedMemberFilter === member.canonical_name;
                                    const profileHref = `/people/${member.slug || slugify(member.canonical_name)}`;
                                    return (
                                        <div
                                            key={member.person_id}
                                            className={`w-full flex items-center gap-3 p-2 rounded-xl text-left border transition-all duration-200 ${
                                                isFiltered
                                                    ? "bg-[#c4ebde]/50 border-[#12362e]/30 shadow-inner"
                                                    : "bg-transparent border-transparent hover:bg-white/60 hover:border-outline-variant/30"
                                            }`}
                                        >
                                            <button
                                                type="button"
                                                onClick={() => {
                                                    setSelectedMemberFilter(isFiltered ? null : member.canonical_name);
                                                    setIsStreamCollapsed(false);
                                                    setTimeout(() => {
                                                        const element = document.getElementById("correspondence-stream");
                                                        if (element) {
                                                            element.scrollIntoView({
                                                                behavior: "smooth",
                                                                block: "start"
                                                            });
                                                        }
                                                    }, 100);
                                                }}
                                                className="min-w-0 flex flex-1 items-center gap-3 text-left"
                                                aria-pressed={isFiltered}
                                                aria-label={`Filter correspondence by ${displayContactName(member.canonical_name, member.primary_email)}`}
                                            >
                                                <div
                                                    className={`w-8 h-8 rounded-lg flex items-center justify-center font-bold text-xs select-none flex-shrink-0 ${getPastelColor(idx)}`}>
                                                    {member.avatar_url ? (
                                                        <img src={member.avatar_url}
                                                             alt={displayContactName(member.canonical_name, member.primary_email)}
                                                             className="w-full h-full rounded-lg object-cover"/>
                                                    ) : (
                                                        contactInitials(displayContactName(member.canonical_name, member.primary_email))
                                                    )}
                                                </div>
                                                <div className="min-w-0 flex-1">
                                                    <div className="flex items-center gap-1.5 flex-wrap">
                                                        <p className="text-ui-medium font-bold text-on-surface truncate font-ui-medium leading-none">
                                                            {displayContactName(member.canonical_name, member.primary_email)}
                                                        </p>
                                                    </div>
                                                    <p className="text-[10px] text-on-surface-variant font-ui-small leading-none mt-1">
                                                        {member.role}
                                                    </p>
                                                </div>
                                            </button>
                                            <Link
                                                href={profileHref}
                                                className="text-primary hover:text-primary-container transition-colors inline-flex align-middle flex-shrink-0 p-1"
                                                title={`Open ${displayContactName(member.canonical_name, member.primary_email)}`}
                                                aria-label={`Open ${displayContactName(member.canonical_name, member.primary_email)} profile`}
                                            >
                                                <span
                                                    className="material-symbols-outlined text-[13px] opacity-75">open_in_new</span>
                                            </Link>
                                        </div>
                                    );
                                })}
                            </div>
                        </div>
                    )}

                    {/* Decisions */}
                    {project.decisions && project.decisions.length > 0 && (
                        <div
                            className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 space-y-4 shadow-sm">
                            <div
                                className="flex items-center gap-2 border-b border-outline-variant/40 pb-2 text-on-surface-variant">
                                <span className="material-symbols-outlined text-lg">gavel</span>
                                <h3 className="text-label-caps font-bold text-primary">DECISIONS</h3>
                            </div>
                            <div className="space-y-3">
                                {project.decisions.map((decision, idx) => (
                                    <div key={idx}
                                         className="bg-white border border-outline-variant/40 rounded-xl p-3.5 space-y-1 shadow-sm">
                                        <p className="text-[10px] text-on-surface-variant font-mono uppercase tracking-wider font-semibold">
                                            {decision.title}
                                        </p>
                                        <div className="flex items-center gap-2">
                                            <span className="w-1.5 h-1.5 rounded-full bg-primary flex-shrink-0"></span>
                                            <p className="text-ui-medium font-bold text-on-surface leading-tight font-ui-medium">
                                                {decision.value}
                                            </p>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}

                    {/* Expenses */}
                    {project.expenses && project.expenses.length > 0 && (
                        <div
                            className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 space-y-4 shadow-sm">
                            <div
                                className="flex items-center gap-2 border-b border-outline-variant/40 pb-2 text-on-surface-variant">
                                <span className="material-symbols-outlined text-lg">payments</span>
                                <h3 className="text-label-caps font-bold text-primary">EXPENSES</h3>
                            </div>
                            <div className="space-y-2 font-mono text-ui-small">
                                {project.expenses.map((expense, idx) => {
                                    const isTotal = expense.category.toLowerCase().includes("total") || expense.category.toLowerCase().includes("net");
                                    if (isTotal) return null;
                                    return (
                                        <div key={idx}
                                             className="flex justify-between items-center text-on-surface-variant">
                                            <span>{expense.category}</span>
                                            <span className="font-bold text-on-surface">{expense.amount}</span>
                                        </div>
                                    );
                                })}
                                {/* Render the summary row */}
                                {(() => {
                                    const totalExpense = project.expenses.find(e => e.category.toLowerCase().includes("total") || e.category.toLowerCase().includes("net"));
                                    if (!totalExpense) return null;
                                    return (
                                        <div
                                            className="border-t border-b border-outline-variant/40 py-2.5 mt-3 flex justify-between items-center text-ui-medium font-bold text-primary font-ui-medium">
                                            <span>{totalExpense.category}</span>
                                            <span>{totalExpense.amount}</span>
                                        </div>
                                    );
                                })()}
                            </div>
                        </div>
                    )}

                    {/* Attachments */}
                    {project.attachments && project.attachments.length > 0 && (
                        <div
                            className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 space-y-4 shadow-sm">
                            <div
                                className="flex items-center gap-2 border-b border-outline-variant/40 pb-2 text-on-surface-variant">
                                <span className="material-symbols-outlined text-lg">description</span>
                                <h3 className="text-label-caps font-bold text-primary">ATTACHMENTS</h3>
                            </div>
                            <div className="space-y-2.5">
                                {project.attachments.map((attachment, idx) => {
                                    const name = attachment.name;
                                    const lowerName = name.toLowerCase();
                                    let iconName = "description";
                                    if (lowerName.endsWith(".pdf")) iconName = "picture_as_pdf";
                                    else if (lowerName.endsWith(".csv") || lowerName.endsWith(".xlsx") || lowerName.endsWith(".xls")) iconName = "table_chart";
                                    else if (lowerName.endsWith(".doc") || lowerName.endsWith(".docx")) iconName = "article";

                                    return (
                                        <div key={idx}
                                             className="flex items-center gap-2.5 text-ui-medium text-on-surface hover:text-primary transition-colors cursor-pointer group">
                      <span
                          className="material-symbols-outlined text-on-surface-variant group-hover:text-primary transition-colors text-[20px]">
                        {iconName}
                      </span>
                                            <span className="truncate group-hover:underline">{name}</span>
                                        </div>
                                    );
                                })}
                            </div>
                        </div>
                    )}

                </aside>
            </div>

            {/* Floating Hover Citation Tooltip */}
            {tooltipPos && hoveredMessage && (
                <div
                    role="button"
                    tabIndex={0}
                    aria-label={`Open message ${hoveredMessage.message_id}`}
                    onMouseEnter={clearTooltipHideTimer}
                    onMouseLeave={scheduleCitationTooltipHide}
                    onClick={handleTooltipOpenMessage}
                    onKeyDown={handleTooltipKeyDown}
                    className="fixed z-50 -translate-x-1/2 -translate-y-full bg-inverse-surface text-inverse-on-surface p-4 rounded-xl shadow-lg border border-outline w-80 max-w-[90vw] text-left transition-all duration-200 cursor-pointer"
                    style={{top: tooltipPos.top, left: tooltipPos.left}}
                >
                    <div className="flex justify-between items-center gap-2 mb-1.5 border-b border-outline/30 pb-1.5">
            <span
                className="rounded-full border border-outline/20 bg-white/10 px-2 py-0.5 text-[10px] font-medium text-white/90">
              {formatEvidenceLabel(hoveredMessage.message_id)}
            </span>
                        <span className="text-[10px] opacity-75 font-mono">
              {hoveredMessage.date}
            </span>
                    </div>
                    <p className="text-[11px] font-bold truncate mb-1">
                        {maskEmailAddresses(hoveredMessage.from_canonical_name)} &rarr; {hoveredMessage.direction === "from_account" ? "Outbound" : "Inbound"}
                    </p>
                    <p className="text-xs font-bold line-clamp-1 mb-1.5 text-white">
                        {maskEmailAddresses(hoveredMessage.subject)}
                    </p>
                    <p className="text-[11px] opacity-90 line-clamp-3 italic font-serif">
                        &ldquo;{maskEmailAddresses(hoveredMessage.snippet)}&rdquo;
                    </p>
                    <div
                        className="absolute left-1/2 bottom-0 -translate-x-1/2 translate-y-full w-0 h-0 border-x-[6px] border-x-transparent border-t-[6px] border-t-inverse-surface"></div>
                </div>
            )}
        </div>
    );
}
