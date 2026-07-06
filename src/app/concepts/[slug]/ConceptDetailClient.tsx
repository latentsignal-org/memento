"use client";

import {useMemo, useState} from "react";
import Link from "next/link";
import {useRouter} from "next/navigation";
import {revalidateEntityPath} from "@/app/actions";
import ConceptCompileButton from "@/components/agent/ConceptCompileButton";
import HowThisWasBuiltPanel from "@/components/agent/HowThisWasBuiltPanel";
import {buildCitationIndexMap, CitationChipList, EvidenceText,} from "@/components/evidence/EvidenceCitations";
import InspectorTabs from "@/components/evidence/InspectorTabs";
import {MessagePreview} from "@/components/evidence/MessagePreview";
import type {MessageSummary} from "@/components/evidence/types";
import {useMessageDetail} from "@/components/evidence/useMessageDetail";
import {contactInitials, displayContactName, maskEmailAddresses} from "@/lib/contact-display";
import {avatarUrl, initialsFromName} from "@/lib/avatar";
import {formatMonthDay} from "@/lib/date-utils";

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

interface ConceptPageData {
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

interface ConceptDetailClientProps {
    concept: ConceptPageData;
    simulationMode?: boolean;
    simulationDelayMs?: number | null;
}

export default function ConceptDetailClient({
                                                concept,
                                                simulationMode = false,
                                                simulationDelayMs = null,
                                            }: ConceptDetailClientProps) {
    const router = useRouter();
    const [isEditingTitle, setIsEditingTitle] = useState(false);
    const [titleDraft, setTitleDraft] = useState(concept.name);
    const [titleValue, setTitleValue] = useState(concept.name);
    const [titleError, setTitleError] = useState<string | null>(null);
    const [activeRailTab, setActiveRailTab] = useState<"context" | "source">("context");
    const [selectedMessageId, setSelectedMessageId] = useState<number | null>(null);
    const [highlightedMessageId, setHighlightedMessageId] = useState<number | null>(null);
    const [isGenerating, setIsGenerating] = useState(false);
    const timelineById = useMemo(
        () => new Map(concept.timeline.map((item) => [item.message_id, item])),
        [concept.timeline],
    );
    const messageSummaries = useMemo(() => {
        const map = new Map<number, MessageSummary>();
        concept.timeline.forEach((item) => {
            map.set(item.message_id, {
                messageId: item.message_id,
                subject: item.subject,
                snippet: item.snippet,
                sentAt: item.date,
                fromLabel: item.from_canonical_name,
                directionLabel: item.is_newsletter ? "Newsletter" : undefined,
            });
        });
        return map;
    }, [concept.timeline]);
    const selectedMessage = selectedMessageId ? timelineById.get(selectedMessageId) ?? null : null;
    const {detail: selectedMessageDetail, isLoading: selectedMessageLoading, error: selectedMessageError} =
        useMessageDetail(selectedMessageId);
    const hasNarrative =
        !!concept.narrative?.scope_summary ||
        (concept.narrative?.distilled_insights && concept.narrative.distilled_insights.length > 0) ||
        !!concept.narrative?.evolving_understanding;
    const citationIndexMap = useMemo(
        () =>
            buildCitationIndexMap([
                concept.narrative.scope_summary || "",
                ...(concept.narrative.distilled_insights || []).map((insight) => insight.content),
                concept.narrative.evolving_understanding || "",
            ]),
        [concept.narrative.distilled_insights, concept.narrative.evolving_understanding, concept.narrative.scope_summary],
    );

    const openMessageSource = (messageId: number) => {
        if (!messageId) return;
        setSelectedMessageId(messageId);
        setActiveRailTab("source");
    };

    const locateMessageInMentions = (messageId: number) => {
        setHighlightedMessageId(messageId);
        const element = document.getElementById(`concept-msg-${messageId}`);
        if (element) {
            const rect = element.getBoundingClientRect();
            const absoluteTop = window.scrollY + rect.top;
            const topOffset = Math.max(120, window.innerHeight * 0.18);
            window.scrollTo({
                top: Math.max(0, absoluteTop - topOffset),
                behavior: "smooth",
            });
        }
        setTimeout(() => {
            setHighlightedMessageId((current) => (current === messageId ? null : current));
        }, 3000);
    };

    const saveTitle = async () => {
        const name = titleDraft.trim();
        if (!name || name === titleValue) {
            setIsEditingTitle(false);
            setTitleDraft(titleValue);
            return;
        }
        setTitleError(null);
        const res = await fetch(`/api/concepts/${concept.slug}`, {
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
        void revalidateEntityPath(`/concepts/${concept.slug}`)
            .catch((error) => console.error("revalidate concept path", error))
            .finally(() => {
                window.location.reload();
            });
    };

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div className="max-w-[1280px] mx-auto px-6 py-10">
                <Link href="/concepts"
                      className="inline-flex items-center gap-1 text-ui-small text-on-surface-variant hover:text-primary mb-6">
                    <span className="material-symbols-outlined text-base">arrow_back</span>
                    Back to Concepts
                </Link>
                {simulationMode && (
                    <div
                        className="mb-6 rounded-lg border border-amber-300/80 bg-amber-50 px-4 py-3 text-[12px] font-semibold text-amber-900">
                        Simulation mode: generation runs in harness mode (no LLM token usage).
                    </div>
                )}

                <header className="border-b border-outline-variant pb-10 mb-10">
                    <div className="flex items-start justify-between gap-8 flex-wrap">
                        <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-3 mb-2">
                <span
                    className={`px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider ${
                        simulationMode ? "bg-amber-600 text-white" : "bg-primary text-white"
                    }`}
                >
                  Concept
                </span>
                                {simulationMode && (
                                    <span
                                        className="bg-amber-100 text-amber-900 border border-amber-300/80 px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">
                    Sim
                  </span>
                                )}
                                <span
                                    className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px]">
                  {concept.status.toUpperCase()}
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
                                <h1 className="text-display-lg font-display-lg text-primary tracking-tight flex items-center gap-2">
                                    <span>{maskEmailAddresses(titleValue)}</span>
                                    <button type="button" onClick={() => setIsEditingTitle(true)}
                                            className="text-on-surface-variant hover:text-primary">
                                        <span className="material-symbols-outlined text-base">edit</span>
                                    </button>
                                </h1>
                            )}
                            {concept.scope_description ? (
                                <p className="mt-3 text-body-reading font-body-reading text-on-surface-variant leading-relaxed max-w-[720px]">
                                    {maskEmailAddresses(concept.scope_description)}
                                </p>
                            ) : null}
                            {concept.seed_keywords.length > 0 ? (
                                <div className="flex flex-wrap gap-2 mt-4">
                                    {concept.seed_keywords.map((keyword) => (
                                        <span
                                            key={keyword}
                                            className="bg-surface-container-highest border border-outline-variant text-on-surface-variant text-[11px] font-medium px-2.5 py-1 rounded-[2px] tracking-wide"
                                        >
                      {keyword}
                    </span>
                                    ))}
                                </div>
                            ) : null}
                        </div>
                        <div className="flex flex-col items-end gap-2 flex-shrink-0">
                            <div
                                className="bg-surface-container-low border border-outline-variant rounded-[2px] px-3 py-1.5 flex items-center gap-2">
                                <span
                                    className="material-symbols-outlined text-on-surface-variant text-sm">database</span>
                                <span className="text-label-caps font-label-caps text-on-surface-variant">
                  {concept.message_count.toLocaleString()} SOURCES
                </span>
                            </div>
                            <div className="text-[12px] text-on-surface-variant opacity-60">
                                {formatMonthDay(concept.date_range.first)} — {formatMonthDay(concept.date_range.last)}
                            </div>
                        </div>
                    </div>
                </header>

                <div className="grid grid-cols-1 lg:grid-cols-3 gap-12 items-start">
                    <div className="lg:col-span-2 space-y-12">
                        <section
                            className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 sm:p-8 shadow-sm space-y-4">
                            <div
                                className="grid grid-cols-1 gap-4 border-b border-outline-variant/40 pb-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-start">
                                <div>
                                    <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px]">
                                        WHAT THIS CONCEPT COVERS
                                    </h3>
                                    <p className="mt-2 text-ui-small text-on-surface-variant">
                                        Create a concise concept brief from source messages and citations.
                                    </p>
                                </div>
                                <div className="contents">
                                    <ConceptCompileButton
                                        slug={concept.slug}
                                        hasGenerated={hasNarrative}
                                        onRunningChange={setIsGenerating}
                                        cardLayout="full-row"
                                        simulateByDefault={simulationMode}
                                        simulationDelayMs={simulationDelayMs ?? undefined}
                                    />
                                    {hasNarrative && !isGenerating ? (
                                        <div
                                            className="flex flex-wrap items-center justify-end gap-4 md:col-start-2 md:justify-self-end">
                                            <HowThisWasBuiltPanel
                                                sessionType="concept_compile"
                                                entityId={concept.slug}
                                                provenanceDimension="concepts"
                                                provenanceSlug={concept.slug}
                                                buttonStyle="link"
                                            />
                                        </div>
                                    ) : null}
                                </div>
                            </div>
                            {concept.narrative.scope_summary ? (
                                <p className="font-body-reading text-body-reading text-on-surface leading-relaxed">
                                    <EvidenceText
                                        text={concept.narrative.scope_summary}
                                        citationIndexMap={citationIndexMap}
                                        onSelect={openMessageSource}
                                        messageSummaries={messageSummaries}
                                    />
                                </p>
                            ) : (
                                <p className="text-body-reading font-body-reading text-on-surface-variant italic">
                                    No concept brief yet. Generate one from the attached sources.
                                </p>
                            )}
                        </section>

                        {concept.narrative.distilled_insights?.length ? (
                            <section>
                                <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px] mb-4">
                                    DISTILLED INSIGHTS ({concept.narrative.distilled_insights.length})
                                </h3>
                                <div className="space-y-6">
                                    {concept.narrative.distilled_insights.map((insight, index) => (
                                        <article
                                            key={`${insight.title}-${index}`}
                                            className="bg-surface-container-lowest border border-outline-variant rounded-lg p-6"
                                        >
                                            <div className="flex items-start gap-3 mb-3">
                        <span
                            className="flex-shrink-0 w-7 h-7 rounded-full bg-primary text-white flex items-center justify-center text-[12px] font-bold">
                          {index + 1}
                        </span>
                                                <h4 className="text-headline-md font-headline-md text-primary leading-tight pt-0.5">
                                                    {insight.title}
                                                </h4>
                                            </div>
                                            <p className="text-ui-medium text-on-surface leading-relaxed pl-10">
                                                <EvidenceText
                                                    text={insight.content}
                                                    citationIndexMap={citationIndexMap}
                                                    onSelect={openMessageSource}
                                                    messageSummaries={messageSummaries}
                                                />
                                            </p>
                                            {insight.source_message_ids.length > 0 ? (
                                                <div className="mt-3 pl-10">
                                                    <CitationChipList
                                                        messageIds={insight.source_message_ids.slice(0, 8)}
                                                        citationIndexMap={citationIndexMap}
                                                        onSelect={openMessageSource}
                                                        messageSummaries={messageSummaries}
                                                    />
                                                </div>
                                            ) : null}
                                        </article>
                                    ))}
                                </div>
                            </section>
                        ) : null}

                        {concept.narrative.evolving_understanding ? (
                            <section>
                                <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px] mb-4">
                                    HOW COVERAGE HAS EVOLVED
                                </h3>
                                <p className="font-body-reading text-body-reading text-on-surface leading-relaxed whitespace-pre-wrap">
                                    <EvidenceText
                                        text={concept.narrative.evolving_understanding}
                                        citationIndexMap={citationIndexMap}
                                        onSelect={openMessageSource}
                                        messageSummaries={messageSummaries}
                                    />
                                </p>
                            </section>
                        ) : null}

                        {!hasNarrative ? (
                            <section
                                className="bg-surface-container-low border border-dashed border-outline-variant rounded-lg p-8 text-center space-y-4">
                                <p className="text-ui-medium text-on-surface-variant">
                                    No narrative generated yet.
                                </p>
                            </section>
                        ) : null}

                        <section>
                            <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px] mb-4">
                                MOST RECENT MENTIONS
                            </h3>
                            <ol className="space-y-3">
                                {concept.timeline.slice(0, 10).map((item) => (
                                    <li key={item.message_id}>
                                        <MessagePreview
                                            id={`concept-msg-${item.message_id}`}
                                            messageId={item.message_id}
                                            layout="row"
                                            summary={{
                                                messageId: item.message_id,
                                                subject: item.subject,
                                                snippet: item.snippet,
                                                sentAt: item.date,
                                                dateLabel: formatMonthDay(item.date),
                                            }}
                                            selected={selectedMessageId === item.message_id}
                                            highlighted={highlightedMessageId === item.message_id}
                                            badge={{
                                                label: item.is_newsletter ? "Newsletter" : "Email",
                                                tone: item.is_newsletter ? "neutral" : "inbound",
                                            }}
                                            metadata={
                                                <p className="text-[11px] text-on-surface-variant">
                                                    {item.is_newsletter && item.newsletter_slug ? (
                                                        <Link href={`/newsletters/${item.newsletter_slug}`}
                                                              className="hover:text-primary">
                                                            from {maskEmailAddresses(item.from_canonical_name)}
                                                        </Link>
                                                    ) : (
                                                        <>from {maskEmailAddresses(item.from_canonical_name)}</>
                                                    )}
                                                </p>
                                            }
                                            onOpen={() => openMessageSource(item.message_id)}
                                        />
                                    </li>
                                ))}
                            </ol>
                        </section>
                    </div>

                    <aside
                        className="space-y-6 lg:sticky lg:top-16 lg:max-h-[calc(100vh-4rem)] lg:overflow-y-auto min-w-0 lg:pb-8">
                        <InspectorTabs
                            activeTab={activeRailTab}
                            onChange={(tab) => setActiveRailTab(tab as "context" | "source")}
                            tabs={[
                                {id: "context", label: "Context"},
                                {id: "source", label: "Source", disabled: !selectedMessageId},
                            ]}
                        />

                        {activeRailTab === "source" ? (
                            <div
                                className="rounded-2xl border border-outline-variant/40 bg-surface-container-low overflow-hidden">
                                <div className="px-6 py-5 border-b border-outline-variant/40 bg-surface-container">
                                    <p className="text-[11px] uppercase tracking-[0.14em] text-on-surface-variant mb-2">Source</p>
                                    <h3 className="text-ui-medium font-bold text-primary">Supporting email</h3>
                                </div>
                                <MessagePreview
                                    messageId={selectedMessageDetail?.message_id ?? selectedMessage?.message_id ?? 0}
                                    layout="side-panel"
                                    detail={selectedMessageDetail}
                                    summary={
                                        selectedMessage
                                            ? {
                                                messageId: selectedMessage.message_id,
                                                subject: selectedMessage.subject,
                                                snippet: selectedMessage.snippet,
                                                sentAt: selectedMessage.date,
                                                fromLabel: selectedMessage.from_canonical_name,
                                            }
                                            : null
                                    }
                                    isLoading={selectedMessageLoading}
                                    error={selectedMessageError}
                                    emptyText="Click any citation or mention to inspect the supporting email here."
                                    onLocate={selectedMessage ? locateMessageInMentions : undefined}
                                    locateLabel="Locate in mentions"
                                />
                            </div>
                        ) : (
                            <>
                                {concept.source_map.people?.length ? (
                                    <div>
                                        <h4 className="text-label-caps font-label-caps text-on-surface-variant border-b border-outline-variant pb-2">
                                            PEOPLE
                                        </h4>
                                        <ul className="mt-4 space-y-3">
                                            {concept.source_map.people.map((person) => {
                                                const displayName = displayContactName(person.canonical_name, person.primary_email);
                                                const profileSlug = person.profile_slug || person.slug;
                                                const rowContent = (
                                                    <>
                                                        <div className="flex items-center gap-2 min-w-0">
                                                            <div
                                                                className="w-7 h-7 rounded-full bg-primary text-white flex items-center justify-center text-[10px] font-bold flex-shrink-0 overflow-hidden">
                                                                {person.primary_email ? (
                                                                    <img src={avatarUrl(person.primary_email, 56, initialsFromName(displayName, person.primary_email))}
                                                                         alt={displayName}
                                                                         className="w-full h-full object-cover"/>
                                                                ) : (
                                                                    contactInitials(displayName)
                                                                )}
                                                            </div>
                                                            <span
                                                                className={`text-[12px] font-medium text-on-surface truncate ${person.has_profile ? "group-hover:text-primary" : ""}`}>
                                {displayName}
                              </span>
                                                        </div>
                                                        <span
                                                            className="text-[11px] text-on-surface-variant flex-shrink-0">
                              {person.contributions}
                            </span>
                                                    </>
                                                );

                                                return (
                                                    <li key={person.person_id}>
                                                        {person.has_profile && profileSlug ? (
                                                            <Link
                                                                href={`/people/${profileSlug}`}
                                                                className="flex items-center justify-between gap-3 group hover:bg-surface-container-low rounded p-1 -m-1"
                                                            >
                                                                {rowContent}
                                                            </Link>
                                                        ) : (
                                                            <div
                                                                className="flex items-center justify-between gap-3 rounded p-1 -m-1">
                                                                {rowContent}
                                                            </div>
                                                        )}
                                                    </li>
                                                );
                                            })}
                                        </ul>
                                    </div>
                                ) : null}

                                {concept.source_map.newsletters?.length ? (
                                    <div>
                                        <h4 className="text-label-caps font-label-caps text-on-surface-variant border-b border-outline-variant pb-2">
                                            NEWSLETTERS
                                        </h4>
                                        <ul className="mt-4 space-y-2">
                                            {concept.source_map.newsletters.map((newsletter) => (
                                                <li key={newsletter.slug}>
                                                    <Link
                                                        href={`/newsletters/${newsletter.slug}`}
                                                        className="flex items-center justify-between gap-3 group hover:bg-surface-container-low rounded p-1 -m-1"
                                                    >
                            <span className="text-[12px] font-medium text-on-surface truncate group-hover:text-primary">
                              {maskEmailAddresses(newsletter.display_name)}
                            </span>
                                                        <span
                                                            className="text-[11px] text-on-surface-variant flex-shrink-0">
                              {newsletter.contributions}
                            </span>
                                                    </Link>
                                                </li>
                                            ))}
                                        </ul>
                                    </div>
                                ) : null}

                                <div>
                                    <h4 className="text-label-caps font-label-caps text-on-surface-variant border-b border-outline-variant pb-2">
                                        RANGE
                                    </h4>
                                    <dl className="mt-4 space-y-4">
                                        <div className="flex items-start justify-between gap-3">
                                            <dt className="text-[12px] text-on-surface-variant">First mention</dt>
                                            <dd className="text-[12px] font-bold text-on-surface text-right">
                                                {formatMonthDay(concept.date_range.first)}
                                            </dd>
                                        </div>
                                        <div className="flex items-start justify-between gap-3">
                                            <dt className="text-[12px] text-on-surface-variant">Latest mention</dt>
                                            <dd className="text-[12px] font-bold text-on-surface text-right">
                                                {formatMonthDay(concept.date_range.last)}
                                            </dd>
                                        </div>
                                        <div className="flex items-start justify-between gap-3">
                                            <dt className="text-[12px] text-on-surface-variant">Messages</dt>
                                            <dd className="text-[12px] font-bold text-on-surface text-right">
                                                {concept.message_count.toLocaleString()}
                                            </dd>
                                        </div>
                                    </dl>
                                </div>
                            </>
                        )}
                    </aside>
                </div>
            </div>
        </main>
    );
}
