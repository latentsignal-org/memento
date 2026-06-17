export interface MessageRecipient {
    email: string;
    name?: string;
    type: string;
}

export interface MessageDetail {
    message_id: number;
    subject: string;
    snippet: string;
    body_text: string;
    sent_at: string;
    from_email: string;
    from_name: string;
    conversation_id: number;
    recipients: MessageRecipient[];
    source_message_id?: string;
    source_conversation_id?: string;
    source_type?: string;
    account_email?: string;
    external_url?: string;
}

export interface MessageSummary {
    messageId: number;
    subject: string;
    snippet: string;
    sentAt: string;
    dateLabel?: string;
    fromLabel?: string;
    fromEmail?: string;
    sourceLabel?: string;
    directionLabel?: string;
}
