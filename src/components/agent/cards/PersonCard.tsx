"use client";
import {ArrowDownLeft, ArrowUpRight, Globe, Mail, Sparkles, User} from "lucide-react";
import {formatMonthDay} from "@/lib/date-utils";
import {formatEvidenceLabel} from "@/components/evidence/labels";
import {cleanCardText} from "./card-text";

interface Alias {
    email_address: string;
    display_name: string;
    link_source: string;
}

interface TimelineEntry {
    date: string;
    subject: string;
    snippet: string;
    message_id: number;
    direction: string;
    via_email: string;
}

interface Correspondent {
    person_id: number;
    canonical_name: string;
    primary_email: string;
    shared_count: number;
}

interface PersonFacet {
    id: number;
    facet_type: string;
    content: string;
    confidence: number;
}

interface NarrativeSection {
    content: string;
    source_message_ids: number[];
}

interface PersonNarrative {
    summary?: NarrativeSection;
    relationship_arc?: NarrativeSection;
    current_status?: NarrativeSection;
}

interface AliasesSummary {
    count: number;
}

interface RelationshipSummary {
    total_messages?: number;
    from_contact_count?: number;
    to_contact_count?: number;
    bidirectional_score?: number;
    first_message_at?: string;
    last_message_at?: string;
}

interface SavedGroup {
    group_id: number;
    display_name?: string;
    label?: string;
    size: number;
}

interface PersonCardProps {
    data: {
        person_id: number;
        canonical_name: string;
        primary_email: string;
        domain: string;
        email_count: number;
        total_messages: number;
        from_contact_count: number;
        to_contact_count: number;
        bidirectional_score: number;
        classification: string;
        first_message_at?: string;
        last_message_at?: string;
        aliases?: Alias[];
        recent_timeline_sample?: TimelineEntry[];
        top_correspondents?: Correspondent[];
        facets?: PersonFacet[];
        narrative?: PersonNarrative;
        aliases_summary?: AliasesSummary;
        relationship?: RelationshipSummary;
        social_graph?: {
            structural_role?: string;
            saved_groups?: SavedGroup[];
        };
    };
}

export default function PersonCard({data}: PersonCardProps) {
    const {
        canonical_name,
        primary_email,
        domain,
        total_messages,
        bidirectional_score,
        classification,
        aliases = [],
        recent_timeline_sample = [],
        top_correspondents = [],
        facets = [],
        narrative = {},
        aliases_summary,
        relationship,
        social_graph,
    } = data;

    const effectiveTotalMessages = total_messages ?? relationship?.total_messages ?? 0;
    const effectiveScore = bidirectional_score ?? relationship?.bidirectional_score ?? 0;
    const aliasCount = aliases_summary?.count ?? aliases.length;
    const savedGroups = social_graph?.saved_groups ?? [];
    const scorePct = Math.round(effectiveScore * 100);

    // Group facets by type
    const groupedFacets = facets.reduce<Record<string, PersonFacet[]>>((acc, f) => {
        const t = f.facet_type.replace(/_/g, " ");
        if (!acc[t]) acc[t] = [];
        acc[t].push(f);
        return acc;
    }, {});

    return (
        <div className="space-y-6 animate-fade-in">
            {/* Header Profile */}
            <div
                className="relative overflow-hidden rounded-xl border border-outline-variant/60 bg-gradient-to-br from-primary/5 to-primary-fixed/5 p-5 shadow-sm">
                <div className="flex items-start gap-4">
                    <div
                        className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-primary-fixed text-on-primary-fixed-variant shadow-sm">
                        <User className="h-6 w-6"/>
                    </div>
                    <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex items-center flex-wrap gap-2">
                            <h2 className="text-xl font-bold tracking-tight text-on-surface">
                                {canonical_name}
                            </h2>
                            {classification && (
                                <span
                                    className="inline-flex items-center gap-1 rounded-full bg-primary/10 px-2.5 py-0.5 text-xs font-semibold text-primary capitalize">
                  <Sparkles className="h-3 w-3"/>
                                    {classification}
                </span>
                            )}
                        </div>
                        <p className="flex items-center gap-1.5 text-sm text-on-surface-variant font-medium">
                            <Mail className="h-3.5 w-3.5 shrink-0 text-on-surface-variant/75"/>
                            <span className="truncate">{primary_email}</span>
                        </p>
                        {domain && (
                            <p className="flex items-center gap-1.5 text-xs text-on-surface-variant/80">
                                <Globe className="h-3.5 w-3.5 shrink-0"/>
                                <span>{domain}</span>
                            </p>
                        )}
                    </div>
                </div>

                {/* Stats Grid */}
                <div className="mt-5 grid grid-cols-3 gap-2.5 border-t border-outline-variant/30 pt-4">
                    <div className="rounded bg-surface-container-low p-2.5 text-center">
                        <div
                            className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Messages
                        </div>
                        <div className="mt-1 text-lg font-bold text-on-surface">{effectiveTotalMessages}</div>
                    </div>
                    <div className="rounded bg-surface-container-low p-2.5 text-center">
                        <div
                            className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Score
                        </div>
                        <div className="mt-1 text-lg font-bold text-primary">{scorePct}%</div>
                    </div>
                    <div className="rounded bg-surface-container-low p-2.5 text-center">
                        <div
                            className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Aliases
                        </div>
                        <div className="mt-1 text-lg font-bold text-on-surface">{aliasCount}</div>
                    </div>
                </div>
            </div>

            {savedGroups.length > 0 && (
                <section className="space-y-3">
                    <h3 className="text-sm font-bold uppercase tracking-wider text-on-surface-variant">Saved Groups</h3>
                    <ul className="space-y-2">
                        {savedGroups.map((group) => (
                            <li
                                key={group.group_id}
                                className="flex items-center justify-between gap-3 rounded-lg border border-outline-variant/35 bg-surface-container-low/40 p-2.5"
                            >
                                <div className="min-w-0">
                                    <div className="truncate text-xs font-bold text-on-surface">
                                        {group.display_name || group.label || `Group ${group.group_id}`}
                                    </div>
                                    {social_graph?.structural_role && (
                                        <div className="truncate text-[10px] text-on-surface-variant capitalize">
                                            {social_graph.structural_role}
                                        </div>
                                    )}
                                </div>
                                <span
                                    className="shrink-0 rounded bg-primary/5 border border-primary/20 px-2 py-0.5 text-[10px] font-semibold text-primary">
                  {group.size} members
                </span>
                            </li>
                        ))}
                    </ul>
                </section>
            )}

            {/* Narrative Section */}
            {((narrative.summary && narrative.summary.content) ||
                (narrative.relationship_arc && narrative.relationship_arc.content) ||
                (narrative.current_status && narrative.current_status.content)) && (
                <section className="space-y-4">
                    <h3 className="text-sm font-bold uppercase tracking-wider text-on-surface-variant">Narrative</h3>
                    <div className="space-y-3">
                        {narrative.summary && narrative.summary.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-3.5 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Relationship Summary</h4>
                                <p className="text-sm leading-relaxed text-on-surface">{cleanCardText(narrative.summary.content)}</p>
                            </div>
                        )}
                        {narrative.relationship_arc && narrative.relationship_arc.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-3.5 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Interaction Arc</h4>
                                <p className="text-sm leading-relaxed text-on-surface">{cleanCardText(narrative.relationship_arc.content)}</p>
                            </div>
                        )}
                        {narrative.current_status && narrative.current_status.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-3.5 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Current Status</h4>
                                <p className="text-sm leading-relaxed text-on-surface">{cleanCardText(narrative.current_status.content)}</p>
                            </div>
                        )}
                    </div>
                </section>
            )}

            {/* Facets / Facts */}
            {Object.keys(groupedFacets).length > 0 && (
                <section className="space-y-4">
                    <h3 className="text-sm font-bold uppercase tracking-wider text-on-surface-variant">Extracted
                        Facts</h3>
                    <div className="space-y-3.5">
                        {Object.entries(groupedFacets).map(([type, items]) => (
                            <div key={type} className="space-y-2">
                                <h4 className="text-[11px] font-bold text-on-surface-variant/80 uppercase tracking-widest">{type}</h4>
                                <ul className="space-y-1.5">
                                    {items.map((item) => (
                                        <li
                                            key={item.id}
                                            className="flex items-start gap-2.5 rounded-md border border-outline-variant/30 bg-surface-container-low px-3 py-2 text-sm text-on-surface"
                                        >
                                            <span className="mt-1 flex h-1.5 w-1.5 shrink-0 rounded-full bg-primary"/>
                                            <span className="leading-relaxed">{cleanCardText(item.content)}</span>
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        ))}
                    </div>
                </section>
            )}

            {/* Top Correspondents */}
            {top_correspondents.length > 0 && (
                <section className="space-y-3">
                    <h3 className="text-sm font-bold uppercase tracking-wider text-on-surface-variant">Top Shared
                        Contacts</h3>
                    <ul className="space-y-2">
                        {top_correspondents.map((corr) => (
                            <li
                                key={corr.person_id}
                                className="flex items-center justify-between gap-3 rounded-lg border border-outline-variant/35 bg-surface-container-low/40 p-2.5"
                            >
                                <div className="min-w-0">
                                    <div
                                        className="truncate text-xs font-bold text-on-surface">{corr.canonical_name}</div>
                                    <div
                                        className="truncate text-[10px] text-on-surface-variant font-mono">{corr.primary_email}</div>
                                </div>
                                <span
                                    className="shrink-0 rounded bg-primary/5 border border-primary/20 px-2 py-0.5 text-[10px] font-semibold text-primary">
                  {corr.shared_count} shared
                </span>
                            </li>
                        ))}
                    </ul>
                </section>
            )}

            {/* Timeline sample */}
            {recent_timeline_sample.length > 0 && (
                <section className="space-y-3">
                    <h3 className="text-sm font-bold uppercase tracking-wider text-on-surface-variant">Recent
                        History</h3>
                    <div className="relative border-l border-outline-variant pl-4 space-y-4">
                        {recent_timeline_sample.slice(0, 5).map((entry) => {
                            const SentIcon = entry.direction === "from_contact" ? ArrowDownLeft : ArrowUpRight;
                            return (
                                <div key={entry.message_id} className="relative space-y-1">
                                    {/* Timeline dot */}
                                    <span
                                        className="absolute -left-[23px] top-1 flex h-4.5 w-4.5 items-center justify-center rounded-full bg-surface-container-lowest border border-outline-variant text-on-surface-variant shadow-xs">
                    <SentIcon className="h-3 w-3"/>
                  </span>
                                    <div className="flex items-center justify-between gap-2">
                                        <div className="flex items-center gap-2 min-w-0">
                      <span className="text-[10px] font-semibold text-on-surface-variant">
                        {formatMonthDay(entry.date)}
                      </span>
                                            <span
                                                className="rounded-full border border-outline-variant/20 bg-background px-2 py-0.5 text-[10px] font-medium text-on-surface-variant">
                        {formatEvidenceLabel(entry.message_id)}
                      </span>
                                        </div>
                                        <span className="text-[10px] text-on-surface-variant/70 truncate max-w-[140px]">
                      via {entry.via_email}
                    </span>
                                    </div>
                                    <div className="text-xs font-semibold text-on-surface line-clamp-1">
                                        {cleanCardText(entry.subject || "(No Subject)")}
                                    </div>
                                    <p className="text-xs text-on-surface-variant leading-relaxed line-clamp-2">
                                        {cleanCardText(entry.snippet)}
                                    </p>
                                </div>
                            );
                        })}
                    </div>
                </section>
            )}
        </div>
    );
}
