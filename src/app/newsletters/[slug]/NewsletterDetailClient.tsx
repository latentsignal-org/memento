"use client";

import {useEffect, useMemo, useState} from "react";
import {useRouter} from "next/navigation";
import {revalidateEntityPath} from "@/app/actions";
import {maskEmailAddresses} from "@/lib/contact-display";
import RefreshButton from "@/components/refresh-button";
import {formatMonthDay} from "@/lib/date-utils";
import {buildCitationIndexMap, CitationChipList, EvidenceText,} from "@/components/evidence/EvidenceCitations";
import {MessagePreview} from "@/components/evidence/MessagePreview";
import {bestPreviewExcerpt} from "@/components/evidence/message-utils";
import type {MessageSummary} from "@/components/evidence/types";
import {useMessageDetail} from "@/components/evidence/useMessageDetail";

interface NewsletterPageData {
    source: {
        slug: string;
        display_name: string;
        sender_email: string;
        domain: string;
        message_count: number;
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

interface NewsletterDetailClientProps {
    data: NewsletterPageData;
    simulationMode?: boolean;
    simulationDelayMs?: number | null;
}

export default function NewsletterDetailClient({
                                                   data,
                                                   simulationMode = false,
                                                   simulationDelayMs = null,
                                               }: NewsletterDetailClientProps) {
    const router = useRouter();
    const [clientData, setClientData] = useState(data);
    useEffect(() => {
        setClientData(data);
    }, [data]);
    const narrative = clientData.narrative || {};
    const [highlightedMessageId, setHighlightedMessageId] = useState<number | null>(null);
    const [selectedMessageId, setSelectedMessageId] = useState<number | null>(clientData.timeline[0]?.message_id ?? null);
    const timelineById = useMemo(
        () => new Map(clientData.timeline.map((message) => [message.message_id, message])),
        [clientData.timeline],
    );
    const messageSummaries = useMemo(() => {
        const map = new Map<number, MessageSummary>();
        clientData.timeline.forEach((message) => {
            map.set(message.message_id, {
                messageId: message.message_id,
                subject: message.subject,
                snippet: message.snippet,
                sentAt: message.sent_at,
                fromLabel: clientData.source.domain,
                sourceLabel: clientData.source.domain,
                directionLabel: "Newsletter",
            });
        });
        return map;
    }, [clientData.source.domain, clientData.timeline]);
    const selectedMessage = selectedMessageId ? timelineById.get(selectedMessageId) ?? null : null;
    const {detail: selectedMessageDetail, isLoading: selectedMessageLoading, error: selectedMessageError} =
        useMessageDetail(selectedMessageId);
    const coverageIndexMap = useMemo(
        () => buildCitationIndexMap([narrative.coverage_summary || ""]),
        [narrative.coverage_summary],
    );

    const refreshNewsletterData = async () => {
        await revalidateEntityPath(`/newsletters/${clientData.source.slug}`)
            .catch((error) => console.error("revalidate newsletter path", error));

        const res = await fetch(`/api/newsletters/${clientData.source.slug}`, {cache: "no-store"});
        if (!res.ok) {
            throw new Error(`refresh newsletter detail failed: HTTP ${res.status}`);
        }
        const fresh = (await res.json()) as NewsletterPageData;
        setClientData(fresh);
        if (selectedMessageId === null && fresh.timeline[0]?.message_id) {
            setSelectedMessageId(fresh.timeline[0].message_id);
        }
        window.location.reload();
    };

    const handleCitationClick = (messageId: number) => {
        setSelectedMessageId(messageId);
    };

    const locateMessageInTimeline = (messageId: number) => {
        setHighlightedMessageId(messageId);
        const element = document.getElementById(`newsletter-msg-${messageId}`);
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

    const renderCoverage = (text?: string) => {
        if (!text) {
            return (
                <p className="text-body-reading font-body-reading text-on-surface-variant italic">
                    No newsletter brief yet. Generate one from this source&apos;s messages.
                </p>
            );
        }

        return (
            <div
                className="space-y-4 text-body-reading font-body-reading text-on-surface leading-relaxed [text-wrap:pretty]">
                <p className="whitespace-pre-wrap">
                    <EvidenceText
                        text={text}
                        citationIndexMap={coverageIndexMap}
                        onSelect={handleCitationClick}
                        messageSummaries={messageSummaries}
                    />
                </p>
            </div>
        );
    };

    return (
        <div className="grid grid-cols-1 lg:grid-cols-[minmax(0,1.45fr)_minmax(340px,0.95fr)] gap-10 items-start">
            <section className="space-y-10 min-w-0">
                {simulationMode && (
                    <div
                        className="rounded border border-amber-300 bg-amber-50 px-4 py-3 text-sm font-semibold text-amber-900">
                        Simulation mode: newsletter generation runs in harness mode (no LLM token usage).
                    </div>
                )}
                <article className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-8">
                    <div className="mb-5 grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-start">
                        <div>
                            <h2 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px]">COVERAGE
                                SUMMARY</h2>
                            <p className="mt-2 text-ui-small text-on-surface-variant">
                                Create a concise newsletter brief with themes, notable items, and citations.
                            </p>
                        </div>
                        <RefreshButton
                            label={narrative.coverage_summary ? "Re-generate" : "Generate with AI"}
                            runningLabel="Generating…"
                            endpoint={`/api/newsletters/${clientData.source.slug}/generate`}
                            triggerApiBase=""
                            onSucceeded={refreshNewsletterData}
                            className="w-auto"
                            statusVariant="card"
                            panelWidthClassName="w-full"
                            cardLayout="full-row"
                            successMessage="Newsletter brief updated."
                            simulateByDefault={simulationMode}
                            simulationDelayMs={simulationDelayMs ?? undefined}
                            simulationMessages={[
                                `Loading newsletter source ${clientData.source.slug}…`,
                                "Collecting recent issues and source messages…",
                                "Extracting recurring themes…",
                                "Selecting notable recent items…",
                                "Saved narrative sections.",
                            ]}
                            progressSteps={[
                                {
                                    key: "source",
                                    label: "Source",
                                    patterns: ["starting", "loading newsletter source", "generating narrative"]
                                },
                                {
                                    key: "issues",
                                    label: "Issues",
                                    patterns: ["collecting recent issues", "generating narrative", "saved narrative"]
                                },
                                {
                                    key: "themes",
                                    label: "Themes",
                                    patterns: ["extracting recurring themes", "saved narrative"]
                                },
                                {
                                    key: "items",
                                    label: "Items",
                                    patterns: ["selecting notable recent items", "saved narrative"]
                                },
                                {
                                    key: "summary",
                                    label: "Summary",
                                    patterns: ["saved narrative", "refreshing newsletter report"]
                                },
                            ]}
                        />
                    </div>
                    {renderCoverage(narrative.coverage_summary)}
                </article>

                {narrative.recurring_themes && narrative.recurring_themes.length > 0 && (
                    <section>
                        <h2 className="text-headline-md font-headline-md text-primary mb-5">Recurring Themes</h2>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            {narrative.recurring_themes.map((theme) => (
                                <div key={theme.theme}
                                     className="bg-surface-container-low border border-outline-variant/40 rounded-xl p-5">
                                    <p className="text-ui-medium font-bold text-primary mb-2">{maskEmailAddresses(theme.theme)}</p>
                                    <p className="text-[11px] uppercase tracking-wider text-on-surface-variant mb-3">
                                        {theme.source_message_ids.length} supporting
                                        message{theme.source_message_ids.length === 1 ? "" : "s"}
                                    </p>
                                    <div className="space-y-2 mb-4">
                                        {theme.source_message_ids
                                            .map((id) => timelineById.get(id))
                                            .filter((message): message is NewsletterPageData["timeline"][number] => Boolean(message))
                                            .slice(0, 3)
                                            .map((message) => (
                                                <button
                                                    key={message.message_id}
                                                    type="button"
                                                    onClick={() => handleCitationClick(message.message_id)}
                                                    className="block w-full text-left rounded-lg border border-outline-variant/30 px-3 py-2 hover:border-primary/40 hover:bg-surface-container focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                                                >
                                                    <p className="text-ui-small font-semibold text-on-surface">
                                                        {maskEmailAddresses(message.subject || "(No subject)")}
                                                    </p>
                                                    <p className="text-[11px] text-on-surface-variant mt-1">
                                                        {formatMonthDay(message.sent_at)}
                                                    </p>
                                                </button>
                                            ))}
                                    </div>
                                    <CitationChipList
                                        messageIds={theme.source_message_ids.slice(0, 6)}
                                        onSelect={handleCitationClick}
                                        messageSummaries={messageSummaries}
                                    />
                                </div>
                            ))}
                        </div>
                    </section>
                )}

                <section>
                    <h2 className="text-headline-md font-headline-md text-primary mb-5">Message Timeline</h2>
                    <div className="space-y-3">
                        {clientData.timeline.map((message) => {
                            const isHighlighted = highlightedMessageId === message.message_id;
                            return (
                                <MessagePreview
                                    key={message.message_id}
                                    id={`newsletter-msg-${message.message_id}`}
                                    messageId={message.message_id}
                                    layout="row"
                                    summary={{
                                        messageId: message.message_id,
                                        subject: message.subject,
                                        snippet: bestPreviewExcerpt(message.subject, message.snippet, message.body_text),
                                        sentAt: message.sent_at,
                                        dateLabel: formatMonthDay(message.sent_at),
                                    }}
                                    selected={selectedMessageId === message.message_id}
                                    highlighted={isHighlighted}
                                    badge={{label: "Newsletter", tone: "neutral"}}
                                    metadata={
                                        <p className="text-[11px] text-on-surface-variant">
                                            Source: {clientData.source.domain}
                                        </p>
                                    }
                                    onOpen={() => handleCitationClick(message.message_id)}
                                />
                            );
                        })}
                    </div>
                </section>
            </section>

            <aside
                className="space-y-6 lg:sticky lg:top-16 lg:max-h-[calc(100vh-4rem)] lg:overflow-y-auto min-w-0 lg:pb-8">
                <div className="bg-surface-container-low border border-outline-variant/40 rounded-2xl overflow-hidden">
                    <div className="px-6 py-5 border-b border-outline-variant/40 bg-surface-container">
                        <div className="flex items-start justify-between gap-4">
                            <div>
                                <p className="text-[11px] uppercase tracking-[0.14em] text-on-surface-variant mb-2">Source</p>
                                <h2 className="text-ui-medium font-bold text-primary">Supporting email</h2>
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
                                    sentAt: selectedMessage.sent_at,
                                    fromLabel: clientData.source.domain,
                                    sourceLabel: clientData.source.domain,
                                }
                                : null
                        }
                        isLoading={selectedMessageLoading}
                        error={selectedMessageError}
                        emptyText="Click any citation or timeline row to inspect the supporting email here."
                        onLocate={selectedMessage ? locateMessageInTimeline : undefined}
                        locateLabel="Locate in timeline"
                    />
                </div>

                <div className="bg-primary text-primary-foreground rounded-2xl p-6">
                    <h2 className="text-ui-medium font-bold mb-4">Source Stats</h2>
                    <div className="space-y-4">
                        <div>
                            <p className="text-[11px] uppercase tracking-wider opacity-70">Domain</p>
                            <p className="font-mono text-ui-medium">{clientData.source.domain}</p>
                        </div>
                        <div>
                            <p className="text-[11px] uppercase tracking-wider opacity-70">Messages</p>
                            <p className="text-headline-md font-headline-md">{clientData.source.message_count}</p>
                        </div>
                    </div>
                </div>

                {narrative.notable_recent && narrative.notable_recent.length > 0 && (
                    <div className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6">
                        <h2 className="text-ui-medium font-bold text-primary mb-4">Notable Recent</h2>
                        <div className="space-y-4">
                            {narrative.notable_recent.map((item) => (
                                <div key={item.headline}
                                     className="border-b border-outline-variant/40 last:border-b-0 pb-4 last:pb-0">
                                    <p className="text-ui-small font-bold text-on-surface mb-1">{maskEmailAddresses(item.headline)}</p>
                                    <p className="text-[11px] text-on-surface-variant mb-1">{item.date}</p>
                                    <CitationChipList
                                        messageIds={item.source_message_ids.slice(0, 4)}
                                        onSelect={handleCitationClick}
                                        messageSummaries={messageSummaries}
                                    />
                                </div>
                            ))}
                        </div>
                    </div>
                )}
            </aside>

        </div>
    );
}
