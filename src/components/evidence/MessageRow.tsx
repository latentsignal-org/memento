"use client";

import type {KeyboardEvent, MouseEvent, ReactNode} from "react";
import {maskEmailAddresses} from "@/lib/contact-display";
import {formatEvidenceLabel} from "./labels";

interface MessageRowProps {
    messageId: number;
    subject: string;
    snippet: string;
    dateLabel: string;
    selected?: boolean;
    highlighted?: boolean;
    badge?: {
        label: string;
        tone: "inbound" | "outbound" | "neutral";
    };
    metadata?: ReactNode;
    footer?: ReactNode;
    id?: string;
    onSelect: () => void;
}

const toneClasses = {
    inbound:
        "bg-surface-container-low text-on-surface-variant/85 border border-outline-variant/20",
    outbound:
        "bg-surface-container-low text-on-surface-variant/85 border border-outline-variant/20",
    neutral: "bg-surface-container-low text-on-surface-variant/85 border border-outline-variant/20",
};

export default function MessageRow({
                                       messageId,
                                       subject,
                                       snippet,
                                       dateLabel,
                                       selected = false,
                                       highlighted = false,
                                       badge,
                                       metadata,
                                       footer,
                                       id,
                                       onSelect,
                                   }: MessageRowProps) {
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
        onSelect();
    };

    const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.defaultPrevented || isNestedInteractiveTarget(event.target, event.currentTarget)) {
            return;
        }
        if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onSelect();
        }
    };

    return (
        <div
            id={id}
            role="button"
            tabIndex={0}
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
            {snippet ? (
                <p className="text-[11px] text-on-surface-variant leading-relaxed">
                    {maskEmailAddresses(snippet)}
                </p>
            ) : null}
            {footer ? <div className="mt-2">{footer}</div> : null}
        </div>
    );
}
