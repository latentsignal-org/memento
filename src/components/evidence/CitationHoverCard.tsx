"use client";

import type {KeyboardEvent} from "react";
import {useEffect, useMemo, useState} from "react";
import {maskEmailAddresses} from "@/lib/contact-display";
import {formatEvidenceLabel} from "./labels";

export interface CitationHoverMessage {
    messageId: number;
    dateLabel: string;
    fromLabel: string;
    subject: string;
    snippet: string;
    directionLabel?: string;
}

interface CitationHoverCardProps {
    message: CitationHoverMessage;
    position: { top: number; left: number };
    onOpen: () => void;
    onKeepOpen: () => void;
    onCloseSoon: () => void;
}

export default function CitationHoverCard({
                                              message,
                                              position,
                                              onOpen,
                                              onKeepOpen,
                                              onCloseSoon,
                                          }: CitationHoverCardProps) {
    const [viewportWidth, setViewportWidth] = useState<number>(0);

    useEffect(() => {
        const updateViewportWidth = () => setViewportWidth(window.innerWidth);
        updateViewportWidth();
        window.addEventListener("resize", updateViewportWidth);
        return () => window.removeEventListener("resize", updateViewportWidth);
    }, []);

    const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onOpen();
        }
        if (event.key === "Escape") {
            onCloseSoon();
        }
    };

    const layout = useMemo(() => {
        const maxWidth = viewportWidth > 0 ? Math.min(320, viewportWidth * 0.9) : 320;
        const edgePadding = 12;
        const unclampedLeft = position.left - maxWidth / 2;
        const maxLeft = viewportWidth > 0 ? Math.max(edgePadding, viewportWidth - edgePadding - maxWidth) : unclampedLeft;
        const clampedLeft =
            viewportWidth > 0 ? Math.min(Math.max(unclampedLeft, edgePadding), maxLeft) : unclampedLeft;
        const arrowShift = Math.max(
            -maxWidth / 2 + 24,
            Math.min(maxWidth / 2 - 24, position.left - (clampedLeft + maxWidth / 2)),
        );
        return {maxWidth, clampedLeft, arrowShift};
    }, [position.left, viewportWidth]);

    return (
        <div
            role="button"
            tabIndex={0}
            aria-label={`Open message ${message.messageId}`}
            onMouseEnter={onKeepOpen}
            onMouseLeave={onCloseSoon}
            onClick={onOpen}
            onKeyDown={handleKeyDown}
            className="fixed z-50 -translate-y-full cursor-pointer rounded-xl border border-outline bg-inverse-surface p-4 text-left text-inverse-on-surface shadow-lg transition-all duration-200"
            style={{top: position.top, left: layout.clampedLeft, width: layout.maxWidth, maxWidth: "90vw"}}
        >
            <div className="mb-1.5 flex items-center justify-between gap-2 border-b border-outline/30 pb-1.5">
        <span
            className="rounded-full border border-outline/20 bg-white/10 px-2 py-0.5 text-[10px] font-medium text-white/90">
          {formatEvidenceLabel(message.messageId)}
        </span>
                <span className="font-mono text-[10px] opacity-75">{message.dateLabel}</span>
            </div>
            <p className="mb-1 truncate text-[11px] font-bold">
                {maskEmailAddresses(message.fromLabel)}
                {message.directionLabel ? ` -> ${message.directionLabel}` : ""}
            </p>
            <p className="mb-1.5 line-clamp-1 text-xs font-bold text-white">
                {maskEmailAddresses(message.subject)}
            </p>
            <p className="line-clamp-3 text-[11px] italic opacity-90">
                &ldquo;{maskEmailAddresses(message.snippet)}&rdquo;
            </p>
            <div
                className="absolute bottom-0 left-1/2 h-0 w-0 -translate-x-1/2 translate-y-full border-x-[6px] border-x-transparent border-t-[6px] border-t-inverse-surface"
                style={{marginLeft: `${layout.arrowShift}px`}}
            />
        </div>
    );
}
