import type {MouseEvent} from "react";
import {maskEmailAddresses} from "@/lib/contact-display";
import {formatEvidenceLabel} from "./labels";

interface CitationButtonProps {
    messageId: number;
    label?: number | string;
    disabled?: boolean;
    onSelect?: (messageId: number) => void;
    onHover?: (messageId: number | null, event: MouseEvent<HTMLButtonElement> | null) => void;
}

export function CitationButton({
                                   messageId,
                                   label,
                                   disabled = false,
                                   onSelect,
                                   onHover,
                               }: CitationButtonProps) {
    return (
        <button
            type="button"
            disabled={disabled}
            onClick={() => !disabled && onSelect?.(messageId)}
            onMouseEnter={(event) => !disabled && onHover?.(messageId, event)}
            onMouseLeave={() => !disabled && onHover?.(null, null)}
            className={`px-1.5 py-0.5 mx-0.5 font-mono text-[10px] font-bold rounded transition-all inline-flex items-center align-middle ${
                disabled
                    ? "bg-surface-container text-on-surface-variant/40 border border-outline-variant/30 cursor-not-allowed"
                    : "bg-[#c4ebde] text-[#00201a] hover:bg-[#a9cec2] border border-[#12362e]/10 cursor-pointer shadow-sm"
            }`}
            title={
                disabled
                    ? `${formatEvidenceLabel(messageId)} is unavailable on this page`
                    : `Open ${formatEvidenceLabel(messageId)}`
            }
        >
            [{label ?? messageId}]
        </button>
    );
}

export function CitationChipList({
                                     messageIds,
                                     citationIndexMap,
                                     onSelect,
                                     onHover,
                                 }: {
    messageIds: number[];
    citationIndexMap?: Map<number, number>;
    onSelect?: (messageId: number) => void;
    onHover?: (messageId: number | null, event: MouseEvent<HTMLButtonElement> | null) => void;
}) {
    return (
        <div className="flex flex-wrap gap-1.5">
            {messageIds.map((messageId) => (
                <CitationButton
                    key={messageId}
                    messageId={messageId}
                    label={citationIndexMap?.get(messageId) ?? messageId}
                    onSelect={onSelect}
                    onHover={onHover}
                />
            ))}
        </div>
    );
}

export function EvidenceText({
                                 text,
                                 citationIndexMap,
                                 onSelect,
                                 onHover,
                             }: {
    text: string;
    citationIndexMap?: Map<number, number>;
    onSelect?: (messageId: number) => void;
    onHover?: (messageId: number | null, event: MouseEvent<HTMLButtonElement> | null) => void;
}) {
    if (!text) {
        return null;
    }

    const parts = text.split(/(\[msg:[^\]]+\])/gi);
    return parts.map((part, index) => {
        const match = part.match(/\[msg:([^\]]+)\]/i);
        if (!match) {
            return <span key={`text-${index}`}>{maskEmailAddresses(part)}</span>;
        }
        const messageIds = (match[1].match(/\d+/g) || []).map((rawId) => Number.parseInt(rawId, 10));
        return (
            <span key={`cite-${index}`} className="inline-flex align-middle">
        {messageIds.map((messageId) => (
            <CitationButton
                key={`${index}-${messageId}`}
                messageId={messageId}
                label={citationIndexMap?.get(messageId) ?? messageId}
                onSelect={onSelect}
                onHover={onHover}
            />
        ))}
      </span>
        );
    });
}

export function buildCitationIndexMap(texts: string[]) {
    const ids: number[] = [];
    for (const text of texts) {
        if (!text) continue;
        const matches = text.matchAll(/\[msg:([^\]]+)\]/gi);
        for (const match of matches) {
            const rawIds = match[1].match(/\d+/g) || [];
            for (const rawId of rawIds) {
                const messageId = Number.parseInt(rawId, 10);
                if (!ids.includes(messageId)) {
                    ids.push(messageId);
                }
            }
        }
    }

    const indexMap = new Map<number, number>();
    ids.forEach((messageId, index) => {
        indexMap.set(messageId, index + 1);
    });
    return indexMap;
}
