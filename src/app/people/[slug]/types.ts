// Shared types for the person detail page.

export interface PersonAlias {
    email_address: string;
    display_name: string;
    link_source: string;
    locked: boolean;
}

export interface PersonTimelineItem {
    date: string;
    subject: string;
    snippet: string;
    message_id: number;
    direction: "from_contact" | "to_contact" | string;
    via_email: string;
}

export interface PersonTopCorrespondent {
    person_id: number;
    canonical_name: string;
    primary_email: string;
    avatar_url?: string;
    shared_count: number;
}

export interface PersonFacet {
    id: number;
    facet_type: string;
    content: string;
    source_message_ids: number[];
    confidence: number;
    edited_by: string;
    generated_at: string;
}

export interface PersonAttribute {
    id: number;
    category: string;
    label: string;
    value: string;
    date_value?: string;
    source_message_ids: number[];
    confidence: number;
    edited_by: string;
    generated_at: string;
}

export interface NarrativeSection {
    content: string;
    source_message_ids: number[];
    edited_by?: string;
    generated_at?: string;
}

export interface PersonNarrative {
    summary?: NarrativeSection;
    relationship_arc?: NarrativeSection;
    current_status?: NarrativeSection;
}

export interface PersonNeighbor {
    person_id: number;
    canonical_name: string;
    slug: string;
    primary_email: string;
    direct_count: number;
    co_recipient_count: number;
    thread_count: number;
    to_count: number;
    cc_count: number;
    bcc_count: number;
    weight: number;
    last_ts?: string;
}

export interface PersonNetwork {
    person_id: number;
    structural_role: string;
    degree: number;
    weighted_degree: number;
    dormancy_days?: number | null;
    cluster_id?: number | null;
    cluster_size?: number | null;
    cluster_label: string;
    neighbors: PersonNeighbor[];
}

export interface PersonRecord {
    person_id: number;
    canonical_name: string;
    primary_email: string;
    domain: string;
    email_count: number;
    aliases: PersonAlias[];
    total_messages: number;
    from_contact_count: number;
    to_contact_count: number;
    bidirectional_score: number;
    classification: string;
    first_message_at: string;
    last_message_at: string;
    timeline: PersonTimelineItem[];
    top_correspondents?: PersonTopCorrespondent[];
    facets?: PersonFacet[];
    attributes?: PersonAttribute[];
    narrative?: PersonNarrative;
    avatar_url?: string;
    slug?: string;
}
