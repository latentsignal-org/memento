"use client";

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
}

export function MessagePreview({
                                   messageId,
                                   layout,
                                   summary = null,
                                   detail = null,
                                   isLoading = false,
                                   error = null,
                               }: MessagePreviewProps) {
    if (layout !== "compact") {
        return <UnsupportedPreviewLayout layout={layout}/>;
    }

    const errorMessage = typeof error === "string" ? error : error?.message;
    const errorStatus = typeof error === "string" ? undefined : error?.status;
    const subject = detail?.subject || summary?.subject || "";
    const fromLabel = detail?.from_name || summary?.fromLabel || detail?.from_email || summary?.fromEmail || "";
    const sentAt = detail?.sent_at || summary?.sentAt || "";
    const dateLabel = sentAt ? sentAt.slice(0, 16).replace("T", " ") : "";
    const snippet = bestPreviewExcerpt(subject, detail?.snippet || summary?.snippet, detail?.body_text);
    const directionLabel = summary?.directionLabel;

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

function UnsupportedPreviewLayout({layout}: { layout: MessagePreviewProps["layout"] }) {
    return (
        <span className="block rounded border border-outline-variant bg-surface-container-low p-3 text-xs text-on-surface-variant">
            Message preview layout {layout} is not implemented yet.
        </span>
    );
}

