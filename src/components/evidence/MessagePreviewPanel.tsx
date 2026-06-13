"use client";

import {useEffect, useMemo, useState} from "react";
import {maskEmail, maskEmailAddresses} from "@/lib/contact-display";
import {formatMonthDay} from "@/lib/date-utils";
import {formatEvidenceLabel} from "./labels";
import type {MessageDetail, MessageSummary} from "./types";
import {buildPreviewParagraphs} from "./message-utils";

interface MessagePreviewPanelProps {
    detail: MessageDetail | null;
    summary?: MessageSummary | null;
    isLoading?: boolean;
    error?: string | null;
    emptyText: string;
    onLocate?: (() => void) | null;
    locateLabel?: string;
}

export default function MessagePreviewPanel({
                                                detail,
                                                summary,
                                                isLoading = false,
                                                error = null,
                                                emptyText,
                                                onLocate = null,
                                                locateLabel = "Locate in list",
                                            }: MessagePreviewPanelProps) {
    const [expanded, setExpanded] = useState(false);
    const messageKey = detail?.message_id ?? summary?.messageId ?? null;

    useEffect(() => {
        setExpanded(false);
    }, [messageKey]);

    const subject = detail?.subject || summary?.subject || "(No subject)";
    const sentAt = detail?.sent_at || summary?.sentAt || "";
    const fromLabel =
        maskEmailAddresses(detail?.from_name || summary?.fromLabel || "") ||
        maskEmail(detail?.from_email || summary?.fromEmail || "");
    const paragraphs = useMemo(
        () => buildPreviewParagraphs(detail?.body_text, detail?.snippet || summary?.snippet, expanded),
        [detail?.body_text, detail?.snippet, expanded, summary?.snippet],
    );
    const openLabel =
        detail?.source_type === "gmail" ? "Open in Gmail" : "Open original";

    if (!detail && !summary && !isLoading && !error) {
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
                            {maskEmailAddresses(subject)}
                        </p>
                    </div>
                    <span
                        className="shrink-0 inline-flex items-center rounded-full bg-primary-fixed px-2.5 py-1 text-[11px] font-semibold text-on-primary-fixed">
            {formatEvidenceLabel(detail?.message_id ?? summary?.messageId ?? "")}
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
                            onClick={onLocate}
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
                {isLoading && !detail ? (
                    <p className="text-ui-small text-on-surface-variant">Loading email...</p>
                ) : error ? (
                    <p className="text-ui-small text-destructive">{error}</p>
                ) : (
                    <div className="space-y-3 text-[13px] leading-6 text-on-surface">
                        {paragraphs.paragraphs.map((paragraph, index) => (
                            paragraph.startsWith("- ") ? (
                                <div
                                    key={`${detail?.message_id ?? summary?.messageId}-${index}`}
                                    className="rounded-lg border border-outline-variant/20 bg-surface-container-low px-3 py-2"
                                >
                                    <ul className="space-y-1.5">
                                        {paragraph.split("\n").map((line, lineIndex) => (
                                            <li
                                                key={`${detail?.message_id ?? summary?.messageId}-${index}-${lineIndex}`}
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
                                    key={`${detail?.message_id ?? summary?.messageId}-${index}`}
                                    className="whitespace-pre-wrap break-words [overflow-wrap:anywhere] text-pretty"
                                >
                                    {maskEmailAddresses(paragraph)}
                                </p>
                            )
                        ))}
                    </div>
                )}
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
