"use client";

import type {ReactNode} from "react";
import {MessagePreview} from "./MessagePreview";

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
    return (
        <MessagePreview
            id={id}
            messageId={messageId}
            layout="row"
            summary={{
                messageId,
                subject,
                snippet,
                sentAt: "",
                dateLabel,
            }}
            selected={selected}
            highlighted={highlighted}
            badge={badge}
            metadata={metadata}
            footer={footer}
            onOpen={onSelect}
        />
    );
}
