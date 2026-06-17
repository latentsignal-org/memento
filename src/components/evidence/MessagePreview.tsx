"use client";

import type {KeyboardEvent, MouseEvent, ReactNode} from "react";
import {useMemo, useState} from "react";
import {LoaderCircle} from "lucide-react";
import {maskEmail, maskEmailAddresses} from "@/lib/contact-display";
import {formatMonthDay} from "@/lib/date-utils";
import {formatEvidenceLabel} from "./labels";
import {bestPreviewExcerpt, buildPreviewParagraphs} from "./message-utils";
import type {MessageDetail, MessageSummary} from "./types";
import type {MessageReferenceError} from "./useMessageReferenceData";

export interface MessagePreviewProps {
    messageId: number;
    layout: "compact" | "row" | "inline-expanded" | "side-panel";
    summary?: MessageSummary | null;
    detail?: MessageDetail | null;
    isLoading?: boolean;
    error?: MessageReferenceError | string | null;
    onOpen?: (messageId: number) => void;
    onLocate?: (messageId: number) => void;
    externalUrl?: string;
    initiallyExpanded?: boolean;
    showActions?: boolean;
    selected?: boolean;
    highlighted?: boolean;
    badge?: {
        label: string;
        tone: "inbound" | "outbound" | "neutral";
    };
    metadata?: ReactNode;
    footer?: ReactNode;
    id?: string;
    emptyText?: string;
    locateLabel?: string;
}

export function MessagePreview({
                                   messageId,
                                   layout,
                                   summary = null,
                                   detail = null,
                                   isLoading = false,
                                   error = null,
                                   onOpen,
                                   onLocate,
                                   selected = false,
                                   highlighted = false,
                                   badge,
                                   metadata,
                                   footer,
                                   id,
                                   emptyText = "Click a message to inspect the supporting email here.",
                                   locateLabel = "Locate in list",
                                   showActions = false,
                               }: MessagePreviewProps) {
    const errorMessage = typeof error === "string" ? error : error?.message;
    const errorStatus = typeof error === "string" ? undefined : error?.status;
    const subject = detail?.subject || summary?.subject || "";
    const fromLabel = detail?.from_name || summary?.fromLabel || detail?.from_email || summary?.fromEmail || "";
    const sentAt = detail?.sent_at || summary?.sentAt || "";
    const dateLabel = summary?.dateLabel || (sentAt ? sentAt.slice(0, 16).replace("T", " ") : "");
    const snippet = bestPreviewExcerpt(subject, detail?.snippet || summary?.snippet, detail?.body_text);
    const directionLabel = summary?.directionLabel;
    const messageKey = detail?.message_id ?? summary?.messageId ?? messageId;
    const [expandedState, setExpandedState] = useState({messageKey, expanded: false});
    const expanded = expandedState.messageKey === messageKey ? expandedState.expanded : false;
    const setExpandedForCurrentMessage = (next: (current: boolean) => boolean) => {
        setExpandedState({messageKey, expanded: next(expanded)});
    };

    const paragraphs = useMemo(
        () => buildPreviewParagraphs(detail?.body_text, detail?.snippet || summary?.snippet, expanded),
        [detail?.body_text, detail?.snippet, expanded, summary?.snippet],
    );

    if (layout === "row") {
        return (
            <MessagePreviewRow
                id={id}
                messageId={messageId}
                subject={subject}
                snippet={snippet}
                dateLabel={dateLabel}
                selected={selected}
                highlighted={highlighted}
                badge={badge}
                metadata={metadata}
                footer={footer}
                errorMessage={errorMessage}
                onOpen={onOpen}
            />
        );
    }

    if (layout !== "compact") {
        if (layout === "inline-expanded") {
            return (
                <MessagePreviewInlineExpanded
                    messageId={messageId}
                    detail={detail}
                    summary={summary}
                    isLoading={isLoading}
                    errorMessage={errorMessage}
                    subject={subject}
                    sentAt={sentAt}
                    fromLabel={fromLabel}
                    paragraphs={paragraphs}
                    expanded={expanded}
                    setExpanded={setExpandedForCurrentMessage}
                    showActions={showActions}
                />
            );
        }
        if (layout === "side-panel") {
            return (
                <MessagePreviewSidePanel
                    messageId={messageId}
                    detail={detail}
                    summary={summary}
                    isLoading={isLoading}
                    errorMessage={errorMessage}
                    emptyText={emptyText}
                    onLocate={onLocate}
                    locateLabel={locateLabel}
                    subject={subject}
                    sentAt={sentAt}
                    paragraphs={paragraphs}
                    expanded={expanded}
                    setExpanded={setExpandedForCurrentMessage}
                />
            );
        }
        return <UnsupportedPreviewLayout layout={layout}/>;
    }

    return (
        <span
            role="tooltip"
            aria-label={`Preview ${formatEvidenceLabel(messageId)}`}
            className="block rounded-xl border border-outline bg-inverse-surface p-4 text-left text-inverse-on-surface shadow-lg"
        >
            {isLoading ? (
                <span className="flex items-center gap-2 text-xs text-inverse-on-surface/80">
                    <LoaderCircle className="h-3.5 w-3.5 animate-spin"/>
                    Loading preview...
                </span>
            ) : errorMessage ? (
                <span className="block space-y-1.5">
                    <span className="block text-xs font-bold text-white">{formatEvidenceLabel(messageId)}</span>
                    <span className="block text-xs text-red-100">
                        {errorStatus === 404 ? "Source message not found." : errorMessage}
                    </span>
                    {errorStatus ? (
                        <span className="block font-mono text-[10px] text-white/70">HTTP {errorStatus}</span>
                    ) : null}
                </span>
            ) : (
                <span className="block">
                    <span className="mb-1.5 flex items-center justify-between gap-2 border-b border-outline/30 pb-1.5">
                        <span className="rounded-full border border-outline/20 bg-white/10 px-2 py-0.5 text-[10px] font-medium text-white/90">
                            {formatEvidenceLabel(messageId)}
                        </span>
                        <span className="font-mono text-[10px] text-white/75">{dateLabel}</span>
                    </span>
                    <span className="mb-1 block truncate text-[11px] font-bold">
                        {maskEmailAddresses(fromLabel)}
                        {directionLabel ? ` -> ${directionLabel}` : ""}
                    </span>
                    <span className="mb-1.5 block line-clamp-1 text-xs font-bold text-white">
                        {maskEmailAddresses(subject || "(No subject)")}
                    </span>
                    <span className="block line-clamp-3 text-[11px] italic text-white/90">
                        &ldquo;{maskEmailAddresses(snippet || "No preview text available for this message yet.")}&rdquo;
                    </span>
                </span>
            )}
        </span>
    );
}

function MessagePreviewInlineExpanded({
                                          messageId,
                                          detail,
                                          summary,
                                          isLoading,
                                          errorMessage,
                                          subject,
                                          sentAt,
                                          fromLabel,
                                          paragraphs,
                                          expanded,
                                          setExpanded,
                                          showActions,
                                      }: {
    messageId: number;
    detail?: MessageDetail | null;
    summary?: MessageSummary | null;
    isLoading: boolean;
    errorMessage?: string;
    subject: string;
    sentAt: string;
    fromLabel: string;
    paragraphs: ReturnType<typeof buildPreviewParagraphs>;
    expanded: boolean;
    setExpanded: (next: (current: boolean) => boolean) => void;
    showActions?: boolean;
}) {
    const displayMessageId = detail?.message_id ?? summary?.messageId ?? messageId;
    const openLabel = detail?.source_type === "gmail" ? "Open in Gmail" : "Open original";

    return (
        <div className="rounded-xl border border-outline-variant/35 bg-background p-4 text-on-surface shadow-xs">
            <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                    <p className="mb-1 text-[10px] uppercase tracking-[0.12em] text-on-surface-variant">
                        {formatEvidenceLabel(displayMessageId)}
                    </p>
                    <h4 className="text-sm font-bold text-primary text-balance">
                        {maskEmailAddresses(subject || "(No subject)")}
                    </h4>
                    <p className="mt-1 text-[11px] text-on-surface-variant">
                        {maskEmailAddresses(fromLabel)}
                        {sentAt ? ` · ${formatMonthDay(sentAt)}` : ""}
                    </p>
                </div>
                {showActions && detail?.external_url ? (
                    <a
                        href={detail.external_url}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex shrink-0 items-center rounded-full border border-outline-variant/50 bg-surface-container-high px-3 py-1 text-[11px] font-semibold text-primary hover:bg-primary-fixed/35 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                    >
                        {openLabel}
                    </a>
                ) : null}
            </div>
            <MessagePreviewBody
                displayMessageId={displayMessageId}
                isLoading={isLoading}
                hasDetail={Boolean(detail)}
                errorMessage={errorMessage}
                paragraphs={paragraphs}
            />
            {paragraphs.truncated ? (
                <button
                    type="button"
                    onClick={() => setExpanded((current) => !current)}
                    className="mt-4 inline-flex items-center rounded-full bg-surface-container-high px-3 py-1 text-[11px] font-semibold text-primary hover:bg-primary-fixed/35 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                >
                    {expanded ? "Show less" : "Show more"}
                </button>
            ) : null}
        </div>
    );
}

function MessagePreviewSidePanel({
                                     messageId,
                                     detail,
                                     summary,
                                     isLoading,
                                     errorMessage,
                                     emptyText,
                                     onLocate,
                                     locateLabel,
                                     subject,
                                     sentAt,
                                     paragraphs,
                                     expanded,
                                     setExpanded,
                                 }: {
    messageId: number;
    detail?: MessageDetail | null;
    summary?: MessageSummary | null;
    isLoading: boolean;
    errorMessage?: string;
    emptyText: string;
    onLocate?: (messageId: number) => void;
    locateLabel: string;
    subject: string;
    sentAt: string;
    paragraphs: ReturnType<typeof buildPreviewParagraphs>;
    expanded: boolean;
    setExpanded: (next: (current: boolean) => boolean) => void;
}) {
    const hasMessage = Boolean(detail || summary);
    const fromLabel =
        maskEmailAddresses(detail?.from_name || summary?.fromLabel || "") ||
        maskEmail(detail?.from_email || summary?.fromEmail || "");
    const openLabel = detail?.source_type === "gmail" ? "Open in Gmail" : "Open original";
    const displayMessageId = detail?.message_id ?? summary?.messageId ?? messageId;

    if (!hasMessage && !isLoading && !errorMessage) {
        return (
            <div className="p-6 text-ui-small text-on-surface-variant">
                {emptyText}
            </div>
        );
    }

    return (
        <div className="p-6 space-y-5">
            <div className="rounded-xl border border-outline-variant/35 bg-background px-4 py-4 space-y-3">
                <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                        <p className="mb-2 text-[11px] uppercase tracking-[0.12em] text-on-surface-variant">
                            Subject
                        </p>
                        <p className="text-ui-medium font-bold text-primary text-balance">
                            {maskEmailAddresses(subject || "(No subject)")}
                        </p>
                    </div>
                    <span
                        className="shrink-0 inline-flex items-center rounded-full bg-primary-fixed px-2.5 py-1 text-[11px] font-semibold text-on-primary-fixed">
                        {formatEvidenceLabel(displayMessageId)}
                    </span>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-[11px] text-on-surface-variant">
                    <div>
                        <p className="mb-1 uppercase tracking-[0.12em]">From</p>
                        <p className="text-on-surface">{fromLabel}</p>
                    </div>
                    <div>
                        <p className="mb-1 uppercase tracking-[0.12em]">Date</p>
                        <p className="text-on-surface">{formatMonthDay(sentAt)}</p>
                    </div>
                </div>
                {detail?.recipients?.length ? (
                    <div className="text-[11px] text-on-surface-variant">
                        <p className="mb-1 uppercase tracking-[0.12em]">Recipients</p>
                        <p className="text-on-surface">
                            {detail.recipients
                                .map((recipient) =>
                                    maskEmailAddresses(recipient.name || "") || maskEmail(recipient.email),
                                )
                                .join(", ")}
                        </p>
                    </div>
                ) : null}
                <div className="flex flex-wrap gap-2 pt-1">
                    {onLocate ? (
                        <button
                            type="button"
                            onClick={() => onLocate(displayMessageId)}
                            className="inline-flex items-center rounded-full bg-primary-fixed px-3 py-1 text-[11px] font-semibold text-on-primary-fixed hover:bg-primary-fixed-dim focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                        >
                            {locateLabel}
                        </button>
                    ) : null}
                    {detail?.external_url ? (
                        <a
                            href={detail.external_url}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center rounded-full border border-outline-variant/50 bg-surface-container-high px-3 py-1 text-[11px] font-semibold text-primary hover:bg-primary-fixed/35 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                        >
                            {openLabel}
                        </a>
                    ) : null}
                </div>
            </div>

            <div className="rounded-xl border border-outline-variant/35 bg-background px-5 py-5">
                <div
                    className="mb-4 flex items-center gap-2 text-[11px] uppercase tracking-[0.12em] text-on-surface-variant">
                    <span className="inline-block h-2 w-2 rounded-full bg-primary"/>
                    Email excerpt
                </div>
                <MessagePreviewBody
                    displayMessageId={displayMessageId}
                    isLoading={isLoading}
                    hasDetail={Boolean(detail)}
                    errorMessage={errorMessage}
                    paragraphs={paragraphs}
                />
                {paragraphs.truncated ? (
                    <button
                        type="button"
                        onClick={() => setExpanded((current) => !current)}
                        className="mt-4 inline-flex items-center rounded-full bg-surface-container-high px-3 py-1 text-[11px] font-semibold text-primary hover:bg-primary-fixed/35 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                    >
                        {expanded ? "Show less" : "Show more"}
                    </button>
                ) : null}
            </div>
        </div>
    );
}

function MessagePreviewBody({
                                displayMessageId,
                                isLoading,
                                hasDetail,
                                errorMessage,
                                paragraphs,
                            }: {
    displayMessageId: number;
    isLoading: boolean;
    hasDetail: boolean;
    errorMessage?: string;
    paragraphs: ReturnType<typeof buildPreviewParagraphs>;
}) {
    if (isLoading && !hasDetail) {
        return <p className="text-ui-small text-on-surface-variant">Loading email...</p>;
    }
    if (errorMessage) {
        return <p className="text-ui-small text-destructive">{errorMessage}</p>;
    }
    return (
        <div className="space-y-3 text-[13px] leading-6 text-on-surface">
            {paragraphs.paragraphs.map((paragraph, index) => (
                paragraph.startsWith("- ") ? (
                    <div
                        key={`${displayMessageId}-${index}`}
                        className="rounded-lg border border-outline-variant/20 bg-surface-container-low px-3 py-2"
                    >
                        <ul className="space-y-1.5">
                            {paragraph.split("\n").map((line, lineIndex) => (
                                <li
                                    key={`${displayMessageId}-${index}-${lineIndex}`}
                                    className="flex items-start gap-2 break-words text-[13px] leading-6 text-on-surface"
                                >
                                    <span className="mt-[9px] h-1.5 w-1.5 rounded-full bg-primary/70"/>
                                    <span className="[overflow-wrap:anywhere]">
                                        {maskEmailAddresses(line.replace(/^- /, ""))}
                                    </span>
                                </li>
                            ))}
                        </ul>
                    </div>
                ) : (
                    <p
                        key={`${displayMessageId}-${index}`}
                        className="whitespace-pre-wrap break-words [overflow-wrap:anywhere] text-pretty"
                    >
                        {maskEmailAddresses(paragraph)}
                    </p>
                )
            ))}
        </div>
    );
}

const toneClasses = {
    inbound:
        "bg-surface-container-low text-on-surface-variant/85 border border-outline-variant/20",
    outbound:
        "bg-surface-container-low text-on-surface-variant/85 border border-outline-variant/20",
    neutral: "bg-surface-container-low text-on-surface-variant/85 border border-outline-variant/20",
};

function MessagePreviewRow({
                               id,
                               messageId,
                               subject,
                               snippet,
                               dateLabel,
                               selected,
                               highlighted,
                               badge,
                               metadata,
                               footer,
                               errorMessage,
                               onOpen,
                           }: {
    id?: string;
    messageId: number;
    subject: string;
    snippet: string;
    dateLabel: string;
    selected: boolean;
    highlighted: boolean;
    badge?: MessagePreviewProps["badge"];
    metadata?: ReactNode;
    footer?: ReactNode;
    errorMessage?: string;
    onOpen?: (messageId: number) => void;
}) {
    const isNestedInteractiveTarget = (target: EventTarget | null, currentTarget: Element) => {
        if (!(target instanceof Element)) {
            return false;
        }
        const closestInteractive = target.closest("button, a, input, textarea, select, summary, [role='button']");
        if (!closestInteractive) {
            return false;
        }
        return closestInteractive !== currentTarget;
    };

    const handleClick = (event: MouseEvent<HTMLDivElement>) => {
        if (event.defaultPrevented || isNestedInteractiveTarget(event.target, event.currentTarget)) {
            return;
        }
        onOpen?.(messageId);
    };

    const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.defaultPrevented || isNestedInteractiveTarget(event.target, event.currentTarget)) {
            return;
        }
        if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onOpen?.(messageId);
        }
    };

    return (
        <div
            id={id}
            role={onOpen ? "button" : undefined}
            tabIndex={onOpen ? 0 : undefined}
            onClick={handleClick}
            onKeyDown={handleKeyDown}
            className={`w-full rounded-xl border p-4 text-left transition-all duration-300 hover:shadow-md ${
                selected || highlighted
                    ? "ring-2 ring-primary/20 bg-primary-fixed/15 border-primary/35 shadow-md"
                    : "border-outline-variant/30 bg-surface-container-lowest"
            }`}
        >
            <div className="mb-2 flex flex-wrap items-start justify-between gap-2">
                <div className="flex flex-wrap items-center gap-1.5">
                    {badge ? (
                        <span
                            className={`inline-flex items-center gap-0.5 rounded-full px-2 py-0.5 text-[9px] font-medium uppercase tracking-[0.07em] ${toneClasses[badge.tone]}`}
                        >
                            {badge.label}
                        </span>
                    ) : null}
                    <span
                        className="rounded-full border border-outline-variant/20 bg-background px-2 py-0.5 text-[10px] font-medium text-on-surface-variant">
                        {formatEvidenceLabel(messageId)}
                    </span>
                </div>
                <span
                    className="rounded border border-outline-variant/30 bg-surface-container px-1.5 py-0.5 text-[10px] font-mono font-bold text-on-surface-variant">
                    {dateLabel}
                </span>
            </div>

            <p className="mb-1 text-ui-medium font-bold text-on-surface leading-snug">
                {maskEmailAddresses(subject || "(no subject)")}
            </p>
            {metadata ? <div className="mb-2">{metadata}</div> : null}
            {errorMessage ? (
                <p className="text-[11px] text-destructive leading-relaxed">{errorMessage}</p>
            ) : snippet ? (
                <p className="text-[11px] text-on-surface-variant leading-relaxed">
                    {maskEmailAddresses(snippet)}
                </p>
            ) : null}
            {footer ? <div className="mt-2">{footer}</div> : null}
        </div>
    );
}

function UnsupportedPreviewLayout({layout}: { layout: MessagePreviewProps["layout"] }) {
    return (
        <span className="block rounded border border-outline-variant bg-surface-container-low p-3 text-xs text-on-surface-variant">
            Message preview layout {layout} is not implemented yet.
        </span>
    );
}
