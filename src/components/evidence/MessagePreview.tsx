"use client";

import type {KeyboardEvent, MouseEvent, ReactNode} from "react";
import {LoaderCircle} from "lucide-react";
import {maskEmailAddresses} from "@/lib/contact-display";
import {formatEvidenceLabel} from "./labels";
import {bestPreviewExcerpt} from "./message-utils";
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
}

export function MessagePreview({
                                   messageId,
                                   layout,
                                   summary = null,
                                   detail = null,
                                   isLoading = false,
                                   error = null,
                                   onOpen,
                                   selected = false,
                                   highlighted = false,
                                   badge,
                                   metadata,
                                   footer,
                                   id,
                               }: MessagePreviewProps) {
    const errorMessage = typeof error === "string" ? error : error?.message;
    const errorStatus = typeof error === "string" ? undefined : error?.status;
    const subject = detail?.subject || summary?.subject || "";
    const fromLabel = detail?.from_name || summary?.fromLabel || detail?.from_email || summary?.fromEmail || "";
    const sentAt = detail?.sent_at || summary?.sentAt || "";
    const dateLabel = summary?.dateLabel || (sentAt ? sentAt.slice(0, 16).replace("T", " ") : "");
    const snippet = bestPreviewExcerpt(subject, detail?.snippet || summary?.snippet, detail?.body_text);
    const directionLabel = summary?.directionLabel;

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
