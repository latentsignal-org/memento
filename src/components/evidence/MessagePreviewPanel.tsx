"use client";

import {MessagePreview} from "./MessagePreview";
import type {MessageDetail, MessageSummary} from "./types";

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
                                                summary = null,
                                                isLoading = false,
                                                error = null,
                                                emptyText,
                                                onLocate = null,
                                                locateLabel = "Locate in list",
                                            }: MessagePreviewPanelProps) {
    return (
        <MessagePreview
            messageId={detail?.message_id ?? summary?.messageId ?? 0}
            layout="side-panel"
            detail={detail}
            summary={summary}
            isLoading={isLoading}
            error={error}
            emptyText={emptyText}
            onLocate={onLocate ? () => onLocate() : undefined}
            locateLabel={locateLabel}
        />
    );
}
