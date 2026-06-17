import {maskEmailAddresses} from "@/lib/contact-display";
import {formatEvidenceLabel} from "./labels";
import {MessageReference} from "./MessageReference";
import type {MessageSummary} from "./types";

interface CitationButtonProps {
    messageId: number;
    label?: number | string;
    disabled?: boolean;
    onSelect?: (messageId: number) => void;
    summary?: MessageSummary | null;
}

export function CitationButton({
                                   messageId,
                                   label,
                                   disabled = false,
                                   onSelect,
                                   summary = null,
                               }: CitationButtonProps) {
    if (disabled) {
        return (
            <span
                className="px-1.5 py-0.5 mx-0.5 font-mono text-[10px] font-bold rounded transition-all inline-flex items-center align-middle bg-surface-container text-on-surface-variant/40 border border-outline-variant/30 cursor-not-allowed"
                title={`${formatEvidenceLabel(messageId)} is unavailable on this page`}
            >
                [{label ?? messageId}]
            </span>
        );
    }

    return (
        <MessageReference
            messageId={messageId}
            display="citation-number"
            citationNumber={label ?? messageId}
            preview="compact"
            openTarget={onSelect ? "right-panel" : "none"}
            summary={summary}
            onOpen={onSelect}
        />
    );
}

export function CitationChipList({
                                     messageIds,
                                     citationIndexMap,
                                     onSelect,
                                     messageSummaries,
                                 }: {
    messageIds: number[];
    citationIndexMap?: Map<number, number>;
    onSelect?: (messageId: number) => void;
    messageSummaries?: Map<number, MessageSummary>;
}) {
    return (
        <div className="flex flex-wrap gap-1.5">
            {messageIds.map((messageId) => (
                <CitationButton
                    key={messageId}
                    messageId={messageId}
                    label={citationIndexMap?.get(messageId) ?? messageId}
                    onSelect={onSelect}
                    summary={messageSummaries?.get(messageId) ?? null}
                />
            ))}
        </div>
    );
}

export function EvidenceText({
                                 text,
                                 citationIndexMap,
                                 onSelect,
                                 messageSummaries,
                             }: {
    text: string;
    citationIndexMap?: Map<number, number>;
    onSelect?: (messageId: number) => void;
    messageSummaries?: Map<number, MessageSummary>;
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
                summary={messageSummaries?.get(messageId) ?? null}
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
