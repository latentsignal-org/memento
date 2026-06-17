"use client";

import {type KeyboardEvent, useMemo, useState} from "react";
import {MailOpen} from "lucide-react";
import {formatEvidenceLabel} from "./labels";
import {MessagePreview} from "./MessagePreview";
import type {MessageDetail, MessageSummary} from "./types";
import {type MessageReferenceError, useMessageReferenceData} from "./useMessageReferenceData";

export interface MessageReferenceProps {
    messageId: number;
    display: "citation-number" | "subject" | "message-id" | "link-text";
    citationNumber?: number;
    label?: string;
    preview?: "none" | "compact";
    openTarget?: "none" | "right-panel" | "inline" | "external";
    summary?: MessageSummary | null;
    detail?: MessageDetail | null;
    isLoading?: boolean;
    error?: MessageReferenceError | null;
    onOpen?: (messageId: number) => void;
}

export function MessageReference({
                                     messageId,
                                     display,
                                     citationNumber,
                                     label,
                                     preview = "compact",
                                     openTarget = "none",
                                     summary = null,
                                     detail = null,
                                     isLoading: controlledLoading,
                                     error: controlledError,
                                     onOpen,
                                 }: MessageReferenceProps) {
    const needsMessageData = display === "subject" || preview === "compact" || openTarget === "external";
    const fetched = useMessageReferenceData(messageId, {
        enabled: needsMessageData,
        summary,
        detail,
    });
    const effectiveDetail = detail ?? fetched.detail ?? null;
    const effectiveSummary = summary ?? fetched.summary ?? null;
    const effectiveLoading = controlledLoading ?? fetched.isLoading;
    const effectiveError = controlledError ?? fetched.error ?? null;
    const [previewOpen, setPreviewOpen] = useState(false);
    const sourceMissing = effectiveError?.status === 404;
    const previewUnavailable = effectiveError !== null && !sourceMissing;

    const referenceLabel = useMemo(() => {
        if (sourceMissing) {
            return `Message #${messageId} (not found)`;
        }
        if (previewUnavailable && display === "subject") {
            return `Message #${messageId} (preview unavailable)`;
        }
        if (display === "citation-number") {
            return `[${citationNumber ?? messageId}]`;
        }
        if (display === "subject") {
            return effectiveDetail?.subject || effectiveSummary?.subject || formatEvidenceLabel(messageId);
        }
        if (display === "message-id") {
            return formatEvidenceLabel(messageId);
        }
        return label || formatEvidenceLabel(messageId);
    }, [
        citationNumber,
        display,
        effectiveDetail?.subject,
        effectiveSummary?.subject,
        label,
        messageId,
        previewUnavailable,
        sourceMissing,
    ]);

    const handleOpen = () => {
        if (preview === "compact" && !previewOpen) {
            setPreviewOpen(true);
            return;
        }
        if (openTarget !== "none") {
            onOpen?.(messageId);
        }
    };

    const handleKeyDown = (event: KeyboardEvent<HTMLSpanElement>) => {
        if (event.key === "Escape") {
            setPreviewOpen(false);
            return;
        }
        if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            handleOpen();
        }
    };

    const showPreview = preview === "compact" && previewOpen;
    const isError = Boolean(effectiveError);

    return (
        <span
            className="relative inline-block align-baseline mx-0.5"
            onMouseEnter={() => preview === "compact" && setPreviewOpen(true)}
            onMouseLeave={() => preview === "compact" && setPreviewOpen(false)}
            onFocus={() => preview === "compact" && setPreviewOpen(true)}
            onBlur={() => preview === "compact" && setPreviewOpen(false)}
            onKeyDown={handleKeyDown}
        >
            <span
                role="button"
                tabIndex={0}
                aria-label={`${preview === "compact" ? "Preview" : "Open"} ${formatEvidenceLabel(messageId)}`}
                onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    handleOpen();
                }}
                className={referenceClassName(display, isError)}
            >
                {display === "subject" || display === "message-id" || display === "link-text" ? (
                    <MailOpen className="h-3 w-3 shrink-0"/>
                ) : null}
                {referenceLabel}
            </span>

            {showPreview ? (
                <span className="absolute left-0 bottom-full z-50 block w-80 max-w-[90vw] pb-2">
                    <MessagePreview
                        messageId={messageId}
                        layout="compact"
                        summary={effectiveSummary}
                        detail={effectiveDetail}
                        isLoading={effectiveLoading}
                        error={effectiveError}
                    />
                </span>
            ) : null}
        </span>
    );
}

function referenceClassName(display: MessageReferenceProps["display"], isError: boolean) {
    if (isError) {
        return "inline-flex items-center gap-1 rounded border border-red-200 bg-red-50 px-2 py-0.5 text-[11px] font-semibold text-red-700 cursor-pointer transition hover:bg-red-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-red-400";
    }
    if (display === "citation-number") {
        return "px-1.5 py-0.5 mx-0.5 font-mono text-[10px] font-bold rounded transition-all inline-flex items-center align-middle bg-[#c4ebde] text-[#00201a] hover:bg-[#a9cec2] border border-[#12362e]/10 cursor-pointer shadow-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary";
    }
    if (display === "link-text") {
        return "inline-flex items-center gap-1 font-semibold text-primary hover:underline cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary";
    }
    return "inline-flex items-center gap-1 rounded bg-surface-container hover:bg-surface-container-high px-2 py-0.5 text-[11px] font-semibold text-primary cursor-pointer transition focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary";
}
