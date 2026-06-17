"use client";

import Link from "next/link";
import {useRouter} from "next/navigation";
import type {ReactNode} from "react";
import {useCallback, useEffect, useMemo, useState} from "react";
import {contactInitials, displayContactName, maskEmail, maskEmailAddresses} from "@/lib/contact-display";
import EmailReveal from "@/components/email-reveal";
import {gravatarUrl} from "@/lib/gravatar";
import type {PersonAttribute, PersonNeighbor, PersonNetwork, PersonRecord, PersonTimelineItem} from "./types";
import PersonEnrichButton from "@/components/agent/PersonEnrichButton";
import HowThisWasBuiltPanel from "@/components/agent/HowThisWasBuiltPanel";
import {formatMonthDay, relativeDate} from "@/lib/date-utils";
import {
    buildCitationIndexMap,
    CitationButton,
    CitationChipList,
    EvidenceText,
} from "@/components/evidence/EvidenceCitations";
import InspectorTabs from "@/components/evidence/InspectorTabs";
import {MessagePreview} from "@/components/evidence/MessagePreview";
import type {MessageSummary} from "@/components/evidence/types";
import {useMessageDetail} from "@/components/evidence/useMessageDetail";

interface Props {
    person: PersonRecord;
    network: PersonNetwork | null;
    simulationMode?: boolean;
    simulationDelayMs?: number | null;
}

type EditableNarrativeSection = "summary" | "relationship_arc" | "current_status";

function monthsBetween(a: string, b: string): number {
    if (!a || !b) return 0;
    const ms = Math.abs(new Date(b).getTime() - new Date(a).getTime());
    return Math.max(1, Math.round(ms / (1000 * 60 * 60 * 24 * 30.4)));
}

function inferOrganization(domain: string): string {
    if (!domain) return "External";
    const generic = ["gmail.com", "yahoo.com", "hotmail.com", "outlook.com", "aol.com", "icloud.com", "txt.voice.google.com"];
    if (generic.includes(domain.toLowerCase())) return "Personal";
    const name = domain.split(".")[0];
    return name.charAt(0).toUpperCase() + name.slice(1);
}

function pickRelationshipDescriptor(person: PersonRecord): string {
    switch (person.classification) {
        case "candidate":
            return "Frequent contact";
        case "weak_signal":
            return "Occasional contact";
        case "candidate_inbound_only":
            return "Inbound contact";
        default: {
            const org = inferOrganization(person.domain);
            if (org === "Personal" || org === "External") return "Contact";
            return `Contact at ${org}`;
        }
    }
}

function aliasDisplayLabel(alias: { display_name?: string; email_address: string; link_source: string }): string {
    const dn = (alias.display_name || "").trim();
    if (!dn) return maskEmail(alias.email_address);
    // Trim forwarder parentheticals from the chip label — these still show in
    // the alias card detail; chip stays compact.
    return maskEmailAddresses(dn.replace(/\s*\([^)]+\)\s*$/, "").trim()) || maskEmail(alias.email_address);
}

function normalizeAliasEmail(emailAddress: string): string {
    const email = (emailAddress || "").trim().toLowerCase();
    const at = email.indexOf("@");
    if (at <= 0) return email;
    const local = email.slice(0, at);
    const domain = email.slice(at + 1);
    if (domain === "gmail.com" || domain === "googlemail.com") {
        return `${local.replace(/\./g, "")}@gmail.com`;
    }
    return `${local}@${domain}`;
}

function aliasMeaningfulnessScore(emailAddress: string): number {
    const email = (emailAddress || "").trim().toLowerCase();
    const at = email.indexOf("@");
    if (at <= 0) return -500;

    const local = email.slice(0, at);
    const domain = email.slice(at + 1);
    const nonAlphaCount = (local.match(/[^a-z]/g) || []).length;
    const digitsCount = (local.match(/[0-9]/g) || []).length;
    const separatorCount = (local.match(/[._-]/g) || []).length;
    const digitRatio = local.length > 0 ? digitsCount / local.length : 1;

    let score = 0;

    // Strongly demote auto-generated relay aliases.
    if (domain === "txt.voice.google.com") score -= 240;
    if (/^\d+(?:\.\d+)*$/.test(local)) score -= 180;
    if (local.length >= 32) score -= 90;
    else if (local.length >= 24) score -= 50;
    if (digitRatio >= 0.45) score -= 50;
    if (separatorCount >= 4) score -= 25;
    if (local.includes("+")) score -= 20;
    if (!/[a-z]/.test(local)) score -= 60;
    if (/[a-f0-9]{12,}/.test(local)) score -= 35;

    // Promote human-looking local parts (e.g. "firstname.lastname").
    if (/^[a-z]+(?:[._-][a-z]+)*\d{0,3}$/.test(local)) score += 80;
    if (nonAlphaCount <= 3) score += 15;

    return score;
}

export default function PersonDetailClient({
                                               person,
                                               network,
                                               simulationMode = false,
                                               simulationDelayMs = null,
                                           }: Props) {
    const router = useRouter();
    const displayName = displayContactName(person.canonical_name, person.primary_email);
    const lastInteractionRelative = relativeDate(person.last_message_at);
    const monthsActive = monthsBetween(person.first_message_at, person.last_message_at);
    const descriptor = pickRelationshipDescriptor(person);
    const facets = person.facets ?? [];
    const attributes = person.attributes ?? [];
    const narrative = person.narrative ?? {};

    const [isUpdating, setIsUpdating] = useState(false);
    const [editingNarrative, setEditingNarrative] = useState<EditableNarrativeSection | null>(null);
    const [editingFacetId, setEditingFacetId] = useState<number | null>(null);
    const [editingAttributeId, setEditingAttributeId] = useState<number | null>(null);
    const [savingMemoryKey, setSavingMemoryKey] = useState<string | null>(null);
    const [deletedFacetIds, setDeletedFacetIds] = useState<Set<number>>(new Set());
    const [deletedAttributeIds, setDeletedAttributeIds] = useState<Set<number>>(new Set());
    const [narrativeOverrides, setNarrativeOverrides] = useState<Partial<Record<EditableNarrativeSection, string>>>({});
    const [facetOverrides, setFacetOverrides] = useState<Record<number, string>>({});
    const [attributeOverrides, setAttributeOverrides] = useState<Record<number, {
        label: string;
        value: string;
        date_value?: string
    }>>({});
    const [narrativeDrafts, setNarrativeDrafts] = useState<Partial<Record<EditableNarrativeSection, string>>>({});
    const [facetDrafts, setFacetDrafts] = useState<Record<number, string>>({});
    const [attributeDrafts, setAttributeDrafts] = useState<Record<number, {
        label: string;
        value: string;
        date_value?: string
    }>>({});

    const visibleFacets = useMemo(
        () => facets.filter((facet) => !deletedFacetIds.has(facet.id)),
        [deletedFacetIds, facets],
    );
    const visibleAttributes = useMemo(
        () => attributes.filter((attribute) => !deletedAttributeIds.has(attribute.id)),
        [attributes, deletedAttributeIds],
    );

    const handleOverrideClassification = async (target: "human" | "excluded") => {
        if (!person.slug) return;
        setIsUpdating(true);
        try {
            const res = await fetch(`/api/people/${person.slug}/override-classification`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({classification: target}),
            });
            if (!res.ok) {
                const errorText = await res.text();
                alert(`Failed to update classification: ${errorText}`);
                return;
            }

            const data = await res.json();
            if (data.slug) {
                if (data.slug === person.slug) {
                    window.location.reload();
                } else {
                    router.push(`/people/${data.slug}`);
                }
            } else {
                router.push("/people");
            }
        } catch (err) {
            console.error(err);
            alert(`Error updating classification: ${err instanceof Error ? err.message : String(err)}`);
        } finally {
            setIsUpdating(false);
        }
    };

    const narrativeContent = useMemo(
        () => ({
            summary: narrativeOverrides.summary ?? narrative.summary?.content ?? "",
            relationship_arc: narrativeOverrides.relationship_arc ?? narrative.relationship_arc?.content ?? "",
            current_status: narrativeOverrides.current_status ?? narrative.current_status?.content ?? "",
        }),
        [
            narrative.current_status?.content,
            narrative.relationship_arc?.content,
            narrative.summary?.content,
            narrativeOverrides.current_status,
            narrativeOverrides.relationship_arc,
            narrativeOverrides.summary,
        ],
    );

    const startEditingNarrative = (section: EditableNarrativeSection) => {
        setEditingFacetId(null);
        setEditingAttributeId(null);
        setEditingNarrative(section);
        setNarrativeDrafts((prev) => ({...prev, [section]: narrativeContent[section]}));
    };

    const saveNarrative = async (section: EditableNarrativeSection) => {
        if (!person.slug) return;
        const nextContent = (narrativeDrafts[section] ?? "").trim();
        if (!nextContent) {
            alert("Content cannot be empty.");
            return;
        }

        const key = `narrative:${section}`;
        setSavingMemoryKey(key);
        try {
            const res = await fetch(`/api/people/${person.slug}/memory`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({kind: "narrative", section, content: nextContent}),
            });
            if (!res.ok) {
                const errorText = await res.text();
                alert(`Failed to save section: ${errorText}`);
                return;
            }
            setNarrativeOverrides((prev) => ({...prev, [section]: nextContent}));
            setEditingNarrative(null);
        } catch (err) {
            alert(`Error saving section: ${err instanceof Error ? err.message : String(err)}`);
        } finally {
            setSavingMemoryKey(null);
        }
    };

    const facetContentFor = (facetId: number, fallback: string) => facetOverrides[facetId] ?? fallback;

    const startEditingFacet = (facetId: number, currentContent: string) => {
        setEditingNarrative(null);
        setEditingAttributeId(null);
        setEditingFacetId(facetId);
        setFacetDrafts((prev) => ({...prev, [facetId]: facetContentFor(facetId, currentContent)}));
    };

    const saveFacet = async (facetId: number) => {
        if (!person.slug) return;
        const nextContent = (facetDrafts[facetId] ?? "").trim();
        if (!nextContent) {
            alert("Content cannot be empty.");
            return;
        }

        const key = `facet:${facetId}`;
        setSavingMemoryKey(key);
        try {
            const res = await fetch(`/api/people/${person.slug}/memory`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({kind: "facet", facet_id: facetId, content: nextContent}),
            });
            if (!res.ok) {
                const errorText = await res.text();
                alert(`Failed to save facet: ${errorText}`);
                return;
            }
            setFacetOverrides((prev) => ({...prev, [facetId]: nextContent}));
            setEditingFacetId(null);
        } catch (err) {
            alert(`Error saving facet: ${err instanceof Error ? err.message : String(err)}`);
        } finally {
            setSavingMemoryKey(null);
        }
    };

    const deleteFacet = async (facetId: number) => {
        if (!person.slug) return;
        if (!window.confirm("Delete this facet? This cannot be undone.")) return;

        const key = `facet:${facetId}:delete`;
        setSavingMemoryKey(key);
        try {
            const res = await fetch(`/api/people/${person.slug}/memory`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({kind: "facet_delete", facet_id: facetId, content: "delete"}),
            });
            if (!res.ok) {
                const errorText = await res.text();
                alert(`Failed to delete facet: ${errorText}`);
                return;
            }
            setDeletedFacetIds((prev) => new Set(prev).add(facetId));
            setFacetOverrides((prev) => {
                const next = {...prev};
                delete next[facetId];
                return next;
            });
            setFacetDrafts((prev) => {
                const next = {...prev};
                delete next[facetId];
                return next;
            });
            if (editingFacetId === facetId) {
                setEditingFacetId(null);
            }
        } catch (err) {
            alert(`Error deleting facet: ${err instanceof Error ? err.message : String(err)}`);
        } finally {
            setSavingMemoryKey(null);
        }
    };

    const attributeValueFor = (attribute: PersonAttribute) => attributeOverrides[attribute.id] ?? {
        label: attribute.label,
        value: attribute.value,
        date_value: attribute.date_value,
    };

    const startEditingAttribute = (attribute: PersonAttribute) => {
        setEditingNarrative(null);
        setEditingFacetId(null);
        setEditingAttributeId(attribute.id);
        setAttributeDrafts((prev) => ({...prev, [attribute.id]: attributeValueFor(attribute)}));
    };

    const saveAttribute = async (attributeId: number) => {
        if (!person.slug) return;
        const draft = attributeDrafts[attributeId];
        const label = (draft?.label ?? "").trim();
        const value = (draft?.value ?? "").trim();
        const dateValue = (draft?.date_value ?? "").trim();
        if (!label || !value) {
            alert("Label and value cannot be empty.");
            return;
        }

        const key = `attribute:${attributeId}`;
        setSavingMemoryKey(key);
        try {
            const res = await fetch(`/api/people/${person.slug}/memory`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({
                    kind: "attribute",
                    attribute_id: attributeId,
                    label,
                    value,
                    date_value: dateValue,
                }),
            });
            if (!res.ok) {
                const errorText = await res.text();
                alert(`Failed to save detail: ${errorText}`);
                return;
            }
            setAttributeOverrides((prev) => ({...prev, [attributeId]: {label, value, date_value: dateValue}}));
            setEditingAttributeId(null);
        } catch (err) {
            alert(`Error saving detail: ${err instanceof Error ? err.message : String(err)}`);
        } finally {
            setSavingMemoryKey(null);
        }
    };

    const deleteAttribute = async (attributeId: number) => {
        if (!person.slug) return;
        if (!window.confirm("Delete this detail? This cannot be undone.")) return;

        const key = `attribute:${attributeId}:delete`;
        setSavingMemoryKey(key);
        try {
            const res = await fetch(`/api/people/${person.slug}/memory`, {
                method: "PATCH",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({kind: "attribute_delete", attribute_id: attributeId}),
            });
            if (!res.ok) {
                const errorText = await res.text();
                alert(`Failed to delete detail: ${errorText}`);
                return;
            }
            setDeletedAttributeIds((prev) => new Set(prev).add(attributeId));
            setAttributeOverrides((prev) => {
                const next = {...prev};
                delete next[attributeId];
                return next;
            });
            setAttributeDrafts((prev) => {
                const next = {...prev};
                delete next[attributeId];
                return next;
            });
            if (editingAttributeId === attributeId) {
                setEditingAttributeId(null);
            }
        } catch (err) {
            alert(`Error deleting detail: ${err instanceof Error ? err.message : String(err)}`);
        } finally {
            setSavingMemoryKey(null);
        }
    };

    const hasAgentNarrative =
        !!narrativeContent.summary ||
        !!narrativeContent.relationship_arc ||
        !!narrativeContent.current_status;
    const hasGeneratedEnrichment = visibleFacets.length > 0 || visibleAttributes.length > 0 || hasAgentNarrative;
    // Group timeline by year for the narrative.
    const groupedTimeline = useMemo(() => {
        const groups = new Map<string, PersonTimelineItem[]>();
        person.timeline.forEach((t) => {
            const y = t.date ? new Date(t.date).getFullYear().toString() : "Unknown";
            if (!groups.has(y)) groups.set(y, []);
            groups.get(y)!.push(t);
        });
        return Array.from(groups.entries()).sort(([a], [b]) => Number(b) - Number(a));
    }, [person.timeline]);
    const timelineById = useMemo(
        () => new Map(person.timeline.map((item) => [item.message_id, item])),
        [person.timeline],
    );
    const [activeRailTab, setActiveRailTab] = useState<"context" | "source">("context");
    const [selectedMessageId, setSelectedMessageId] = useState<number | null>(null);
    const [highlightedMessageId, setHighlightedMessageId] = useState<number | null>(null);
    const [isGenerating, setIsGenerating] = useState(false);
    const messageSummaries = useMemo(() => {
        const map = new Map<number, MessageSummary>();
        person.timeline.forEach((item) => {
            const fromContact = item.direction === "from_contact";
            map.set(item.message_id, {
                messageId: item.message_id,
                subject: item.subject,
                snippet: item.snippet,
                sentAt: item.date,
                fromLabel: fromContact ? displayName : "You",
                fromEmail: item.via_email,
                directionLabel: fromContact ? "Inbound" : "Outbound",
            });
        });
        return map;
    }, [displayName, person.timeline]);
    const selectedMessage = selectedMessageId ? timelineById.get(selectedMessageId) ?? null : null;
    const {detail: selectedMessageDetail, isLoading: selectedMessageLoading, error: selectedMessageError} =
        useMessageDetail(selectedMessageId);
    const citationIndexMap = useMemo(
        () =>
            buildCitationIndexMap([
                ...visibleAttributes.map((attribute) => attributeValueFor(attribute).value),
                ...visibleFacets.map((facet) => facetContentFor(facet.id, facet.content)),
                narrativeContent.summary,
                narrativeContent.relationship_arc,
                narrativeContent.current_status,
            ]),
        [
            visibleAttributes,
            visibleFacets,
            attributeOverrides,
            facetOverrides,
            narrativeContent.current_status,
            narrativeContent.relationship_arc,
            narrativeContent.summary,
        ],
    );
    const aliasUsageByEmail = useMemo(() => {
        const counts = new Map<string, number>();
        for (const item of person.timeline) {
            const email = normalizeAliasEmail(item.via_email || "");
            if (!email) continue;
            counts.set(email, (counts.get(email) ?? 0) + 1);
        }
        return counts;
    }, [person.timeline]);
    const sortedAliases = useMemo(() => {
        const primaryEmail = person.primary_email.trim().toLowerCase();
        return [...person.aliases].sort((a, b) => {
            const aEmail = a.email_address.trim().toLowerCase();
            const bEmail = b.email_address.trim().toLowerCase();
            const aIsPrimary = aEmail === primaryEmail;
            const bIsPrimary = bEmail === primaryEmail;
            if (aIsPrimary !== bIsPrimary) return aIsPrimary ? -1 : 1;

            const aMeaning = aliasMeaningfulnessScore(aEmail);
            const bMeaning = aliasMeaningfulnessScore(bEmail);
            if (aMeaning !== bMeaning) return bMeaning - aMeaning;

            const aUsage = aliasUsageByEmail.get(normalizeAliasEmail(aEmail)) ?? 0;
            const bUsage = aliasUsageByEmail.get(normalizeAliasEmail(bEmail)) ?? 0;
            if (aUsage !== bUsage) return bUsage - aUsage;

            if (aEmail.length !== bEmail.length) return aEmail.length - bEmail.length;
            return aEmail.localeCompare(bEmail);
        });
    }, [aliasUsageByEmail, person.aliases, person.primary_email]);
    const dedupedSortedAliases = useMemo(() => {
        const seen = new Set<string>();
        const deduped = [];
        for (const alias of sortedAliases) {
            const key = normalizeAliasEmail(alias.email_address);
            if (seen.has(key)) continue;
            seen.add(key);
            deduped.push(alias);
        }
        return deduped;
    }, [sortedAliases]);
    const shownAliases = useMemo(() => dedupedSortedAliases.slice(0, 4), [dedupedSortedAliases]);
    const remainingAliasCount = dedupedSortedAliases.length - shownAliases.length;
    const groupedAttributes = useMemo(() => {
        const groups = new Map<string, PersonAttribute[]>();
        for (const attribute of visibleAttributes) {
            const key = attribute.category || "fact";
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key)!.push(attribute);
        }
        return Array.from(groups.entries());
    }, [visibleAttributes]);

    const openMessageSource = useCallback((messageId: number) => {
        if (!messageId) return;
        setSelectedMessageId(messageId);
        setActiveRailTab("source");
    }, []);

    const jumpToSection = (sectionId: string) => {
        document.getElementById(sectionId)?.scrollIntoView({behavior: "smooth", block: "start"});
    };

    const locateMessageInTimeline = (messageId: number) => {
        setHighlightedMessageId(messageId);
        const element = document.getElementById(`person-msg-${messageId}`);
        if (element) {
            const rect = element.getBoundingClientRect();
            const absoluteTop = window.scrollY + rect.top;
            const topOffset = Math.max(120, window.innerHeight * 0.18);
            window.scrollTo({
                top: Math.max(0, absoluteTop - topOffset),
                behavior: "smooth",
            });
        }
        setTimeout(() => {
            setHighlightedMessageId((current) => (current === messageId ? null : current));
        }, 3000);
    };

    return (
        <div className="pb-20">
            <div className="max-w-[1280px] mx-auto px-6 pt-8">
                <Link href="/people"
                      className="inline-flex items-center gap-1 text-ui-small text-on-surface-variant hover:text-primary mb-6">
                    <span className="material-symbols-outlined text-base">arrow_back</span>
                    Back to People
                </Link>
                {simulationMode && (
                    <div
                        className="mb-6 rounded-lg border border-amber-300/80 bg-amber-50 px-4 py-3 text-[12px] font-semibold text-amber-900">
                        Simulation mode: generation runs in harness mode (no LLM token usage).
                    </div>
                )}

                {/* HEADER */}
                <header className="border-b border-outline-variant pb-10 mb-10">
                    <div className="flex items-start justify-between gap-8">
                        <div className="flex items-start gap-4 min-w-0">
                            <div
                                className="w-20 h-20 rounded-2xl overflow-hidden bg-surface-container-high border border-outline-variant flex items-center justify-center shrink-0">
                                {person.avatar_url ? (
                                    <img
                                        src={person.avatar_url}
                                        alt={displayName}
                                        className="w-full h-full object-cover"
                                    />
                                ) : (
                                    <span
                                        className="text-primary font-bold text-xl">{contactInitials(displayName)}</span>
                                )}
                            </div>
                            <div className="flex-1 min-w-0">
                                <h1 className="text-display-lg font-display-lg text-primary tracking-tight [overflow-wrap:anywhere]">
                                    {displayName}
                                </h1>
                                <p className="mt-1 text-headline-md font-headline-md italic text-on-surface-variant">
                                    {descriptor}
                                </p>
                                <div className="mt-4 overflow-hidden">
                                    <div className="flex items-center gap-2 flex-nowrap overflow-hidden">
                                        {shownAliases.map((a) => (
                                            <button
                                                key={a.email_address}
                                                title={`${a.link_source} · click to see all aliases`}
                                                onClick={() =>
                                                    document.getElementById("identified-aliases")?.scrollIntoView({
                                                        behavior: "smooth",
                                                        block: "start"
                                                    })
                                                }
                                                className="max-w-[14rem] truncate shrink-0 bg-surface-container-highest border border-outline-variant text-on-surface-variant text-[12px] font-mono px-2.5 py-1 rounded-[2px] tracking-wide cursor-pointer hover:border-primary hover:text-primary transition-colors"
                                            >
                                                {maskEmail(a.email_address)}
                                            </button>
                                        ))}
                                        {remainingAliasCount > 0 && (
                                            <button
                                                onClick={() =>
                                                    document.getElementById("identified-aliases")?.scrollIntoView({
                                                        behavior: "smooth",
                                                        block: "start"
                                                    })
                                                }
                                                className="bg-surface-container-highest border border-outline-variant text-on-surface-variant text-[12px] font-mono px-2.5 py-1 rounded-[2px] tracking-wide cursor-pointer hover:border-primary hover:text-primary transition-colors"
                                            >
                                                +{remainingAliasCount} more
                                            </button>
                                        )}
                                    </div>
                                </div>

                            </div>
                        </div>
                        <div className="flex flex-col items-end gap-2 flex-shrink-0">
                            <div
                                className="inline-flex items-center gap-2 rounded-full border border-outline-variant/60 bg-surface-container px-3 py-1.5 shadow-sm">
                                <span
                                    className="material-symbols-outlined text-on-surface-variant text-sm">database</span>
                                <span className="text-label-caps font-label-caps text-on-surface-variant">
                  {person.total_messages.toLocaleString()} MESSAGES
                </span>
                            </div>
                            <div className="text-[12px] text-on-surface-variant opacity-60">
                                Last contact {lastInteractionRelative}
                            </div>
                            {person.slug && (
                                <div className="mt-2">
                                    {person.classification === "excluded" ? (
                                        <button
                                            onClick={() => handleOverrideClassification("human")}
                                            disabled={isUpdating}
                                            className="inline-flex items-center gap-1.5 rounded-lg border border-primary/20 bg-primary/10 px-3 py-1.5 text-ui-small font-bold text-primary transition hover:bg-primary/20 cursor-pointer disabled:opacity-50 flex items-center"
                                        >
                                            <span
                                                className="material-symbols-outlined text-base mr-1.5">person_add</span>
                                            Include
                                        </button>
                                    ) : (
                                        <button
                                            onClick={() => handleOverrideClassification("excluded")}
                                            disabled={isUpdating}
                                            className="inline-flex items-center gap-1.5 rounded-lg border border-outline-variant/80 bg-surface-container-low px-3 py-1.5 text-ui-small font-bold text-on-surface-variant hover:border-error/30 hover:bg-error/10 hover:text-error transition cursor-pointer disabled:opacity-50 flex items-center"
                                        >
                                            <span
                                                className="material-symbols-outlined text-base mr-1.5">person_remove</span>
                                            Exclude
                                        </button>
                                    )}
                                </div>
                            )}
                        </div>
                    </div>
                </header>

                {/* TWO-COLUMN BODY */}
                <div className="grid grid-cols-1 lg:grid-cols-3 gap-12 items-start">
                    {/* MAIN COLUMN */}
                    <div className="lg:col-span-2 space-y-12">
                        <section
                            className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 sm:p-8 shadow-sm space-y-4">
                            <div
                                className="grid grid-cols-1 gap-4 border-b border-outline-variant/40 pb-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-start">
                                <div>
                                    <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px]">
                                        RELATIONSHIP BRIEF
                                    </h3>
                                    <p className="mt-2 text-ui-small text-on-surface-variant">
                                        Create a concise relationship brief from messages, notes, and citations.
                                    </p>
                                </div>
                                <div className="contents">
                                    {person.slug && (
                                        <PersonEnrichButton
                                            slug={person.slug}
                                            hasGenerated={hasGeneratedEnrichment}
                                            onRunningChange={setIsGenerating}
                                            cardLayout="full-row"
                                            simulateByDefault={simulationMode}
                                            simulationDelayMs={simulationDelayMs ?? undefined}
                                        />
                                    )}
                                    {person.slug && hasGeneratedEnrichment && !isGenerating ? (
                                        <div
                                            className="flex flex-wrap items-center justify-end gap-4 md:col-start-2 md:justify-self-end">
                                            <HowThisWasBuiltPanel sessionType="person_enrich" entityId={person.slug}
                                                                  buttonStyle="link"/>
                                        </div>
                                    ) : null}
                                </div>
                            </div>
                            {narrative.summary?.content ? (
                                <NarrativeBlock
                                    title="Summary"
                                    section={narrative.summary}
                                    content={narrativeContent.summary}
                                    isEditing={editingNarrative === "summary"}
                                    isSaving={savingMemoryKey === "narrative:summary"}
                                    draftValue={narrativeDrafts.summary ?? narrativeContent.summary}
                                    onEditStart={() => startEditingNarrative("summary")}
                                    onDraftChange={(value) => setNarrativeDrafts((prev) => ({...prev, summary: value}))}
                                    onCancelEdit={() => setEditingNarrative(null)}
                                    onSaveEdit={() => void saveNarrative("summary")}
                                    citationIndexMap={citationIndexMap}
                                    onSelectMessage={openMessageSource}
                                    messageSummaries={messageSummaries}
                                />
                            ) : (
                                <p className="text-body-reading font-body-reading text-on-surface-variant italic">
                                    No relationship brief yet. Generate one from this person&apos;s messages and notes.
                                </p>
                            )}
                        </section>

                        {visibleFacets.length > 0 && (
                            <section>
                                <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px] mb-4">
                                    RELATIONSHIP FACETS ({visibleFacets.length})
                                </h3>
                                <div className="grid gap-3 sm:grid-cols-2">
                                    {visibleFacets.slice(0, 12).map((facet) => (
                                        <article
                                            key={facet.id}
                                            className="group bg-surface-container-lowest border border-outline-variant rounded-lg p-4"
                                        >
                                            <div className="mb-2 flex items-center justify-between gap-3">
                        <span className="text-[10px] font-bold uppercase tracking-wider text-primary">
                          {facet.facet_type.replace(/_/g, " ")}
                        </span>
                                                <button
                                                    type="button"
                                                    onClick={() => startEditingFacet(facet.id, facet.content)}
                                                    className="inline-flex items-center justify-center rounded p-1 text-on-surface-variant hover:text-primary hover:bg-surface-container-high opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-opacity"
                                                    title="Edit facet"
                                                    aria-label="Edit facet"
                                                >
                                                    <span className="material-symbols-outlined text-[14px]">edit</span>
                                                </button>
                                            </div>
                                            {editingFacetId === facet.id ? (
                                                <div className="space-y-2">
                          <textarea
                              className="w-full min-h-28 resize-y rounded border border-outline-variant bg-surface-container-low p-2 text-ui-small text-on-surface focus:outline-none focus:ring-2 focus:ring-primary/30"
                              value={facetDrafts[facet.id] ?? facetContentFor(facet.id, facet.content)}
                              onChange={(e) => setFacetDrafts((prev) => ({...prev, [facet.id]: e.target.value}))}
                          />
                                                    <div className="flex items-center justify-end gap-2">
                                                        <button
                                                            type="button"
                                                            onClick={() => void deleteFacet(facet.id)}
                                                            disabled={savingMemoryKey === `facet:${facet.id}:delete`}
                                                            className="rounded border border-error/40 bg-error/10 px-2.5 py-1 text-ui-small font-bold text-error hover:bg-error/20 disabled:opacity-50"
                                                        >
                                                            {savingMemoryKey === `facet:${facet.id}:delete` ? "Deleting..." : "Delete"}
                                                        </button>
                                                        <button
                                                            type="button"
                                                            onClick={() => setEditingFacetId(null)}
                                                            className="text-ui-small text-on-surface-variant hover:text-on-surface"
                                                        >
                                                            Cancel
                                                        </button>
                                                        <button
                                                            type="button"
                                                            onClick={() => void saveFacet(facet.id)}
                                                            disabled={savingMemoryKey === `facet:${facet.id}`}
                                                            className="rounded bg-primary px-2.5 py-1 text-ui-small font-bold text-white disabled:opacity-50"
                                                        >
                                                            {savingMemoryKey === `facet:${facet.id}` ? "Saving..." : "Save"}
                                                        </button>
                                                    </div>
                                                </div>
                                            ) : (
                                                <p className="text-ui-small text-on-surface leading-relaxed">
                                                    <EvidenceText
                                                        text={facetContentFor(facet.id, facet.content)}
                                                        citationIndexMap={citationIndexMap}
                                                        onSelect={openMessageSource}
                                                        messageSummaries={messageSummaries}
                                                    />
                                                </p>
                                            )}
                                            {facet.source_message_ids.length > 0 && (
                                                <div className="mt-3">
                                                    <CitationChipList
                                                        messageIds={facet.source_message_ids.slice(0, 5)}
                                                        citationIndexMap={citationIndexMap}
                                                        onSelect={openMessageSource}
                                                        messageSummaries={messageSummaries}
                                                    />
                                                </div>
                                            )}
                                        </article>
                                    ))}
                                </div>
                            </section>
                        )}

                        {hasAgentNarrative && (
                            <section className="space-y-6">
                                {narrative.relationship_arc?.content && (
                                    <NarrativeBlock
                                        title="Relationship arc"
                                        section={narrative.relationship_arc}
                                        content={narrativeContent.relationship_arc}
                                        isEditing={editingNarrative === "relationship_arc"}
                                        isSaving={savingMemoryKey === "narrative:relationship_arc"}
                                        draftValue={narrativeDrafts.relationship_arc ?? narrativeContent.relationship_arc}
                                        onEditStart={() => startEditingNarrative("relationship_arc")}
                                        onDraftChange={(value) => setNarrativeDrafts((prev) => ({
                                            ...prev,
                                            relationship_arc: value
                                        }))}
                                        onCancelEdit={() => setEditingNarrative(null)}
                                        onSaveEdit={() => void saveNarrative("relationship_arc")}
                                        citationIndexMap={citationIndexMap}
                                        onSelectMessage={openMessageSource}
                                        messageSummaries={messageSummaries}
                                    />
                                )}
                                {narrative.current_status?.content && (
                                    <NarrativeBlock
                                        title="Current status"
                                        section={narrative.current_status}
                                        content={narrativeContent.current_status}
                                        isEditing={editingNarrative === "current_status"}
                                        isSaving={savingMemoryKey === "narrative:current_status"}
                                        draftValue={narrativeDrafts.current_status ?? narrativeContent.current_status}
                                        onEditStart={() => startEditingNarrative("current_status")}
                                        onDraftChange={(value) => setNarrativeDrafts((prev) => ({
                                            ...prev,
                                            current_status: value
                                        }))}
                                        onCancelEdit={() => setEditingNarrative(null)}
                                        onSaveEdit={() => void saveNarrative("current_status")}
                                        citationIndexMap={citationIndexMap}
                                        onSelectMessage={openMessageSource}
                                        messageSummaries={messageSummaries}
                                    />
                                )}
                            </section>
                        )}

                        {/* Overview */}
                        <section className="font-body-reading text-body-reading text-on-surface space-y-5">
                            <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px]">
                                OVERVIEW
                            </h3>
                            <p>
                                {displayName} corresponds with you primarily via{" "}
                                <EmailReveal email={person.primary_email} className="font-mono text-primary"/>
                                {person.email_count > 1 && (
                                    <>
                                        {" "}across <strong>{person.email_count}</strong> identified aliases
                                        {" "}
                                        <button
                                            type="button"
                                            onClick={() => jumpToSection("identified-aliases")}
                                            className="inline-flex items-center rounded-full bg-primary-fixed px-2 py-0.5 text-[10px] font-semibold text-on-primary-fixed hover:bg-primary-fixed-dim"
                                        >
                                            View aliases
                                        </button>
                                    </>
                                )}
                                . The archive holds <strong>{person.total_messages.toLocaleString()}</strong> messages
                                spanning roughly <strong>{monthsActive}</strong> months
                                — <strong>{person.from_contact_count}</strong> inbound
                                and <strong>{person.to_contact_count}</strong> outbound.
                            </p>
                            <p>
                                First contact landed on <strong>{formatMonthDay(person.first_message_at)}</strong>, most
                                recent on <strong>{formatMonthDay(person.last_message_at)}</strong>.
                            </p>
                            {person.top_correspondents && person.top_correspondents.length > 0 && (
                                <p>
                                    Their strongest mutual context is with{" "}
                                    <Link
                                        href={`/people/${slugifyExternal(person.top_correspondents[0].canonical_name)}`}
                                        className="text-primary underline-offset-2 hover:underline"
                                    >
                                        {displayContactName(person.top_correspondents[0].canonical_name, person.top_correspondents[0].primary_email)}
                                    </Link>
                                    {" "}— {person.top_correspondents[0].shared_count} threads in common
                                    {person.timeline[1]?.message_id ? (
                                        <>
                                            {" "}
                                            <CitationButton
                                                messageId={person.timeline[1].message_id}
                                                label={citationIndexMap.get(person.timeline[1].message_id) ?? "source"}
                                                onSelect={openMessageSource}
                                                summary={messageSummaries.get(person.timeline[1].message_id) ?? null}
                                            />
                                        </>
                                    ) : null}
                                    .
                                </p>
                            )}
                        </section>

                        {/* RECENT MESSAGES */}
                        <section id="recent-messages" className="scroll-mt-4">
                            <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px] mb-4">
                                RECENT MESSAGES
                            </h3>
                            <div className="space-y-6">
                                {groupedTimeline.map(([year, items]) => (
                                    <div key={year}>
                                        <div
                                            className="text-[12px] font-bold text-on-surface-variant tracking-wide mb-3 uppercase">
                                            {year}
                                        </div>
                                        <ol className="space-y-3">
                                            {items.slice(0, 8).map((item) => (
                                                <li key={item.message_id}>
                                                    <MessagePreview
                                                        id={`person-msg-${item.message_id}`}
                                                        messageId={item.message_id}
                                                        layout="row"
                                                        summary={{
                                                            messageId: item.message_id,
                                                            subject: item.subject,
                                                            snippet: item.snippet,
                                                            sentAt: item.date,
                                                            dateLabel: formatMonthDay(item.date),
                                                        }}
                                                        selected={selectedMessageId === item.message_id}
                                                        highlighted={highlightedMessageId === item.message_id}
                                                        badge={{
                                                            label: item.direction === "from_contact" ? "Inbound" : "Outbound",
                                                            tone: item.direction === "from_contact" ? "inbound" : "outbound",
                                                        }}
                                                        metadata={
                                                            <p className="text-[11px] text-on-surface-variant">
                                                                via <EmailReveal email={item.via_email}/>
                                                            </p>
                                                        }
                                                        onOpen={() => openMessageSource(item.message_id)}
                                                    />
                                                </li>
                                            ))}
                                        </ol>
                                    </div>
                                ))}
                            </div>
                        </section>

                        {/* ALIASES DETAIL */}
                        <section id="identified-aliases" className="scroll-mt-4">
                            <h3 className="text-label-caps font-label-caps text-on-surface-variant tracking-[1.4px] mb-4">
                                EMAIL ALIASES ({person.email_count})
                            </h3>
                            <ul className="grid sm:grid-cols-2 gap-3">
                                {person.aliases.map((a) => (
                                    <li
                                        key={a.email_address}
                                        className="bg-surface-container-low border border-outline-variant rounded-[3px] p-3"
                                    >
                                        <div className="text-ui-medium font-bold text-primary truncate">
                                            {maskEmailAddresses(a.display_name || "(no display name)")}
                                        </div>
                                        <code className="block text-[11px] text-on-surface-variant truncate mt-0.5">
                                            <EmailReveal email={a.email_address}/>
                                        </code>
                                        <div className="flex gap-2 mt-2 items-center">
                      <span
                          className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant opacity-80">
                        {a.link_source.replace(/_/g, " ")}
                      </span>
                                            {a.locked && (
                                                <span className="text-[10px] text-primary opacity-80">locked</span>
                                            )}
                                        </div>
                                    </li>
                                ))}
                            </ul>
                        </section>
                    </div>

                    {/* RIGHT SIDEBAR */}
                    <aside className="min-w-0 space-y-6 overflow-x-hidden lg:sticky lg:top-16 lg:self-start">
                        <InspectorTabs
                            activeTab={activeRailTab}
                            onChange={(tab) => setActiveRailTab(tab as "context" | "source")}
                            tabs={
                                selectedMessageId
                                    ? [
                                        {id: "context", label: "Context"},
                                        {id: "source", label: "Source"},
                                    ]
                                    : [{id: "context", label: "Context"}]
                            }
                        />

                        {activeRailTab === "source" ? (
                            <div
                                className="rounded-2xl border border-outline-variant/40 bg-surface-container-low overflow-hidden">
                                <div className="px-6 py-5 border-b border-outline-variant/40 bg-surface-container">
                                    <p className="text-[11px] uppercase tracking-[0.14em] text-on-surface-variant mb-2">Source</p>
                                    <h3 className="text-ui-medium font-bold text-primary">Supporting email</h3>
                                </div>
                                <MessagePreview
                                    messageId={selectedMessageDetail?.message_id ?? selectedMessage?.message_id ?? 0}
                                    layout="side-panel"
                                    detail={selectedMessageDetail}
                                    summary={
                                        selectedMessage
                                            ? {
                                                messageId: selectedMessage.message_id,
                                                subject: selectedMessage.subject,
                                                snippet: selectedMessage.snippet,
                                                sentAt: selectedMessage.date,
                                                fromLabel: selectedMessage.direction === "from_contact" ? displayName : "You",
                                                fromEmail: selectedMessage.via_email,
                                            }
                                            : null
                                    }
                                    isLoading={selectedMessageLoading}
                                    error={selectedMessageError}
                                    emptyText="Click a citation or recent message to inspect the supporting email here."
                                    onLocate={selectedMessage ? locateMessageInTimeline : undefined}
                                    locateLabel="Locate in recent messages"
                                />
                            </div>
                        ) : (
                            <>
                                <PersonNotesPanel personId={person.person_id}/>

                                {/* OFTEN ON EMAILS TOGETHER */}
                                {network && network.degree > 0 && (
                                    <NetworkCard network={network}/>
                                )}

                                {visibleAttributes.length > 0 && (
                                    <PersonalDetailsRail
                                        groupedAttributes={groupedAttributes}
                                        attributeDrafts={attributeDrafts}
                                        editingAttributeId={editingAttributeId}
                                        savingMemoryKey={savingMemoryKey}
                                        citationIndexMap={citationIndexMap}
                                        attributeValueFor={attributeValueFor}
                                        onDraftChange={(attributeId, draft) =>
                                            setAttributeDrafts((prev) => ({...prev, [attributeId]: draft}))
                                        }
                                        onEditStart={startEditingAttribute}
                                        onCancelEdit={() => setEditingAttributeId(null)}
                                        onSave={saveAttribute}
                                        onDelete={deleteAttribute}
                                        onSelectMessage={openMessageSource}
                                        messageSummaries={messageSummaries}
                                    />
                                )}

                                {/* VITALS */}
                                <div>
                                    <h4 className="text-label-caps font-label-caps text-on-surface-variant border-b border-outline-variant pb-2">
                                        VITALS
                                    </h4>
                                    <dl className="mt-4 space-y-4">
                                        <Vital label="Last Contact"
                                               onClick={() => document.getElementById("recent-messages")?.scrollIntoView({
                                                   behavior: "smooth",
                                                   block: "start"
                                               })}>
                                            <div className="text-[12px] font-bold text-on-surface text-right">
                                                {lastInteractionRelative}
                                            </div>
                                        </Vital>
                                        <Vital label="First Contact"
                                               onClick={() => document.getElementById("recent-messages")?.scrollIntoView({
                                                   behavior: "smooth",
                                                   block: "start"
                                               })}>
                                            <div className="text-[12px] font-bold text-on-surface text-right">
                                                {formatMonthDay(person.first_message_at)}
                                            </div>
                                        </Vital>
                                        <Vital label="Total Messages">
                                            <div className="text-[12px] font-bold text-on-surface text-right">
                                                {person.total_messages.toLocaleString()}
                                            </div>
                                        </Vital>
                                        <Vital label="Aliases"
                                               onClick={() => document.getElementById("identified-aliases")?.scrollIntoView({
                                                   behavior: "smooth",
                                                   block: "start"
                                               })}>
                                            <div className="text-[12px] font-bold text-on-surface text-right">
                                                {person.email_count}
                                            </div>
                                        </Vital>
                                        <Vital label="Inbound / Outbound">
                                            <div className="text-[12px] font-bold text-on-surface text-right">
                                                {person.from_contact_count.toLocaleString()} / {person.to_contact_count.toLocaleString()}
                                            </div>
                                        </Vital>
                                    </dl>
                                </div>

                                {/* TIMELINE MILESTONES */}
                                <div>
                                    <h4 className="text-label-caps font-label-caps text-on-surface-variant border-b border-outline-variant pb-2">
                                        TIMELINE
                                    </h4>
                                    <div className="relative mt-4 pl-5">
                                        <div className="absolute bg-outline-variant top-1 bottom-1 left-0 w-px"/>
                                        <TimelineDot active label="Most Recent"
                                                     date={formatMonthDay(person.last_message_at)}/>
                                        <TimelineDot label="First Contact"
                                                     date={formatMonthDay(person.first_message_at)}/>
                                    </div>
                                </div>
                            </>
                        )}
                    </aside>
                </div>
            </div>
        </div>
    );
}

function NarrativeBlock({
                            title,
                            section,
                            content,
                            isEditing,
                            isSaving,
                            draftValue,
                            onEditStart,
                            onDraftChange,
                            onCancelEdit,
                            onSaveEdit,
                            citationIndexMap,
                            onSelectMessage,
                            messageSummaries,
                        }: {
    title: string;
    section: { content: string; source_message_ids?: number[] };
    content: string;
    isEditing: boolean;
    isSaving: boolean;
    draftValue: string;
    onEditStart: () => void;
    onDraftChange: (value: string) => void;
    onCancelEdit: () => void;
    onSaveEdit: () => void;
    citationIndexMap: Map<number, number>;
    onSelectMessage: (messageId: number) => void;
    messageSummaries: Map<number, MessageSummary>;
}) {
    return (
        <article className="group bg-surface-container-lowest border border-outline-variant rounded-lg p-5">
            <div className="mb-2 flex items-center justify-between gap-3">
                <h4 className="text-headline-md font-headline-md text-primary">{title}</h4>
                <button
                    type="button"
                    onClick={onEditStart}
                    className="inline-flex items-center justify-center rounded p-1 text-on-surface-variant hover:text-primary hover:bg-surface-container-high opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-opacity"
                    title="Edit section"
                    aria-label="Edit section"
                >
                    <span className="material-symbols-outlined text-[14px]">edit</span>
                </button>
            </div>
            {isEditing ? (
                <div className="space-y-2">
          <textarea
              className="w-full min-h-32 resize-y rounded border border-outline-variant bg-surface-container-low p-3 text-sm leading-6 text-on-surface focus:outline-none focus:ring-2 focus:ring-primary/30"
              value={draftValue}
              onChange={(e) => onDraftChange(e.target.value)}
          />
                    <div className="flex items-center justify-end gap-2">
                        <button
                            type="button"
                            onClick={onCancelEdit}
                            className="text-ui-small text-on-surface-variant hover:text-on-surface"
                        >
                            Cancel
                        </button>
                        <button
                            type="button"
                            onClick={onSaveEdit}
                            disabled={isSaving}
                            className="rounded bg-primary px-2.5 py-1 text-ui-small font-bold text-white disabled:opacity-50"
                        >
                            {isSaving ? "Saving..." : "Save"}
                        </button>
                    </div>
                </div>
            ) : (
                <p className="font-body-reading text-body-reading text-on-surface leading-relaxed whitespace-pre-wrap">
                    <EvidenceText
                        text={content}
                        citationIndexMap={citationIndexMap}
                        onSelect={onSelectMessage}
                        messageSummaries={messageSummaries}
                    />
                </p>
            )}
            {section.source_message_ids && section.source_message_ids.length > 0 && (
                <div className="mt-3">
                    <CitationChipList
                        messageIds={section.source_message_ids.slice(0, 8)}
                        citationIndexMap={citationIndexMap}
                        onSelect={onSelectMessage}
                        messageSummaries={messageSummaries}
                    />
                </div>
            )}
        </article>
    );
}

function PersonalDetailsRail({
                                 groupedAttributes,
                                 attributeDrafts,
                                 editingAttributeId,
                                 savingMemoryKey,
                                 citationIndexMap,
                                 attributeValueFor,
                                 onDraftChange,
                                 onEditStart,
                                 onCancelEdit,
                                 onSave,
                                 onDelete,
                                 onSelectMessage,
                                 messageSummaries,
                             }: {
    groupedAttributes: Array<[string, PersonAttribute[]]>;
    attributeDrafts: Record<number, { label: string; value: string; date_value?: string }>;
    editingAttributeId: number | null;
    savingMemoryKey: string | null;
    citationIndexMap: Map<number, number>;
    attributeValueFor: (attribute: PersonAttribute) => { label: string; value: string; date_value?: string };
    onDraftChange: (attributeId: number, draft: { label: string; value: string; date_value?: string }) => void;
    onEditStart: (attribute: PersonAttribute) => void;
    onCancelEdit: () => void;
    onSave: (attributeId: number) => void;
    onDelete: (attributeId: number) => void;
    onSelectMessage: (messageId: number) => void;
    messageSummaries: Map<number, MessageSummary>;
}) {
    const total = groupedAttributes.reduce((sum, [, items]) => sum + items.length, 0);
    return (
        <div>
            <h4 className="text-label-caps font-label-caps text-on-surface-variant border-b border-outline-variant pb-2">
                PERSONAL DETAILS ({total})
            </h4>
            <div className="mt-4 space-y-4">
                {groupedAttributes.map(([category, items]) => (
                    <div key={category} className="space-y-3">
                        <div className="text-[10px] font-bold uppercase tracking-wider text-primary">
                            {category.replace(/_/g, " ")}
                        </div>
                        <div className="space-y-3">
                            {items.map((attribute) => {
                                const value = attributeValueFor(attribute);
                                const draft = attributeDrafts[attribute.id] ?? value;
                                return (
                                    <article key={attribute.id}
                                             className="group border-b border-outline-variant/50 pb-3 last:border-b-0">
                                        {editingAttributeId === attribute.id ? (
                                            <div className="space-y-2">
                                                <input
                                                    className="w-full rounded border border-outline-variant bg-surface-container-lowest px-2 py-1.5 text-ui-small text-on-surface focus:outline-none focus:ring-2 focus:ring-primary/30"
                                                    value={draft.label}
                                                    onChange={(e) => onDraftChange(attribute.id, {
                                                        ...draft,
                                                        label: e.target.value
                                                    })}
                                                />
                                                <textarea
                                                    className="w-full min-h-20 resize-y rounded border border-outline-variant bg-surface-container-lowest p-2 text-ui-small text-on-surface focus:outline-none focus:ring-2 focus:ring-primary/30"
                                                    value={draft.value}
                                                    onChange={(e) => onDraftChange(attribute.id, {
                                                        ...draft,
                                                        value: e.target.value
                                                    })}
                                                />
                                                <input
                                                    className="w-full rounded border border-outline-variant bg-surface-container-lowest px-2 py-1.5 text-ui-small text-on-surface focus:outline-none focus:ring-2 focus:ring-primary/30"
                                                    placeholder="Date value"
                                                    value={draft.date_value ?? ""}
                                                    onChange={(e) => onDraftChange(attribute.id, {
                                                        ...draft,
                                                        date_value: e.target.value
                                                    })}
                                                />
                                                <div className="flex items-center justify-end gap-2">
                                                    <button
                                                        type="button"
                                                        onClick={() => onDelete(attribute.id)}
                                                        disabled={savingMemoryKey === `attribute:${attribute.id}:delete`}
                                                        className="rounded border border-error/40 bg-error/10 px-2.5 py-1 text-ui-small font-bold text-error hover:bg-error/20 disabled:opacity-50"
                                                    >
                                                        {savingMemoryKey === `attribute:${attribute.id}:delete` ? "Deleting..." : "Delete"}
                                                    </button>
                                                    <button
                                                        type="button"
                                                        onClick={onCancelEdit}
                                                        className="text-ui-small text-on-surface-variant hover:text-on-surface"
                                                    >
                                                        Cancel
                                                    </button>
                                                    <button
                                                        type="button"
                                                        onClick={() => onSave(attribute.id)}
                                                        disabled={savingMemoryKey === `attribute:${attribute.id}`}
                                                        className="rounded bg-primary px-2.5 py-1 text-ui-small font-bold text-white disabled:opacity-50"
                                                    >
                                                        {savingMemoryKey === `attribute:${attribute.id}` ? "Saving..." : "Save"}
                                                    </button>
                                                </div>
                                            </div>
                                        ) : (
                                            <>
                                                <div className="flex items-start justify-between gap-3">
                                                    <div className="min-w-0">
                                                        <div
                                                            className="text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">
                                                            {value.label}
                                                        </div>
                                                        <p className="mt-1 text-ui-small text-on-surface leading-relaxed">
                                                            <EvidenceText
                                                                text={value.value}
                                                                citationIndexMap={citationIndexMap}
                                                                onSelect={onSelectMessage}
                                                                messageSummaries={messageSummaries}
                                                            />
                                                            {value.date_value ? (
                                                                <span
                                                                    className="ml-2 text-[11px] text-on-surface-variant">
                                  {value.date_value}
                                </span>
                                                            ) : null}
                                                        </p>
                                                    </div>
                                                    <button
                                                        type="button"
                                                        onClick={() => onEditStart(attribute)}
                                                        className="inline-flex items-center justify-center rounded p-1 text-on-surface-variant hover:text-primary hover:bg-surface-container-high opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-opacity"
                                                        title="Edit detail"
                                                        aria-label="Edit detail"
                                                    >
                                                        <span
                                                            className="material-symbols-outlined text-[14px]">edit</span>
                                                    </button>
                                                </div>
                                                {attribute.source_message_ids.length > 0 && (
                                                    <div className="mt-2">
                                                        <CitationChipList
                                                            messageIds={attribute.source_message_ids.slice(0, 5)}
                                                            citationIndexMap={citationIndexMap}
                                                            onSelect={onSelectMessage}
                                                            messageSummaries={messageSummaries}
                                                        />
                                                    </div>
                                                )}
                                            </>
                                        )}
                                    </article>
                                );
                            })}
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}

function PersonNotesPanel({personId}: { personId: number }) {
    const [noteId, setNoteId] = useState<number | null>(null);
    const [text, setText] = useState("");
    const [loaded, setLoaded] = useState(false);
    const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">("idle");

    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                const res = await fetch(`/api/notes?dimension=person&entity_id=${personId}`, {
                    cache: "no-store",
                });
                if (!res.ok) throw new Error(`notes: ${res.status}`);
                const data = (await res.json()) as {
                    notes?: Array<{ id: number; content: string }>;
                };
                const first = data.notes?.[0];
                if (!cancelled && first) {
                    setNoteId(first.id);
                    setText(first.content);
                }
                if (!cancelled) setLoaded(true);
            } catch {
                if (!cancelled) {
                    setLoaded(true);
                    setStatus("error");
                }
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [personId]);

    useEffect(() => {
        if (!loaded) return;
        const handle = window.setTimeout(async () => {
            if (!noteId && !text.trim()) {
                setStatus("idle");
                return;
            }
            setStatus("saving");
            try {
                const res = await fetch("/api/notes", {
                    method: noteId ? "PATCH" : "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify(
                        noteId
                            ? {id: noteId, content: text}
                            : {dimension: "person", entity_id: personId, content: text},
                    ),
                });
                if (!res.ok) throw new Error(`save note: ${res.status}`);
                const saved = (await res.json()) as { id: number };
                if (!noteId) setNoteId(saved.id);
                setStatus("saved");
            } catch {
                setStatus("error");
            }
        }, 650);
        return () => window.clearTimeout(handle);
    }, [loaded, noteId, personId, text]);

    return (
        <div>
            <div className="flex items-center justify-between gap-3 border-b border-outline-variant pb-2">
                <h4 className="text-label-caps font-label-caps text-on-surface-variant">NOTES</h4>
                <span className="text-[10px] uppercase tracking-wider text-on-surface-variant">
          {status === "saving" ? "Saving" : status === "saved" ? "Saved" : status === "error" ? "Error" : ""}
        </span>
            </div>
            <textarea
                className="mt-4 min-h-32 w-full resize-y rounded border border-outline-variant bg-surface-container-lowest p-3 text-sm leading-6 text-on-surface shadow-inner focus:outline-none focus:ring-2 focus:ring-primary/30"
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder="Add private context for the person-agent..."
            />
            <p className="mt-2 text-[11px] leading-5 text-on-surface-variant">
                Notes steer the next AI refresh and are treated as authoritative.
            </p>
        </div>
    );
}

function Vital({label, children, onClick}: { label: string; children: ReactNode; onClick?: () => void }) {
    const inner = (
        <>
            <dt className={`text-[12px] text-on-surface-variant${onClick ? " group-hover:text-primary transition-colors" : ""}`}>{label}</dt>
            <dd className="text-right flex flex-col items-end">{children}</dd>
        </>
    );
    if (onClick) {
        return (
            <button onClick={onClick}
                    className="group w-full flex items-start justify-between gap-3 cursor-pointer hover:underline decoration-dotted underline-offset-2 text-left">
                {inner}
            </button>
        );
    }
    return (
        <div className="flex items-start justify-between gap-3">
            {inner}
        </div>
    );
}

function TimelineDot({label, date, active}: { label: string; date: string; active?: boolean }) {
    return (
        <div className={"relative mb-6 " + (active ? "" : "opacity-70")}>
            <div
                className={
                    "absolute -left-[21px] top-[3px] w-2 h-2 rounded-full ring-4 ring-background " +
                    (active ? "bg-primary" : "bg-outline")
                }
            />
            <div className="text-[12px] font-bold text-on-surface">{label}</div>
            <div className="text-[12px] text-on-surface-variant">{date}</div>
        </div>
    );
}

function roleBadgeClass(role: string): string {
    switch (role) {
        case "hub":
            return "bg-primary-fixed text-on-primary-fixed-variant";
        case "bridge":
            return "bg-primary-fixed text-on-primary-fixed-variant";
        default:
            return "bg-surface-container-high text-on-surface-variant";
    }
}

const ROLE_LABEL: Record<string, string> = {
    hub: "Broad overlap",
    bridge: "Group connector",
};

const ROLE_EXPLANATION: Record<string, string> = {
    hub: "Appears across many recurring group conversations.",
    bridge: "Often appears in conversations that connect otherwise separate groups.",
};

function NetworkCard({network}: { network: PersonNetwork }) {
    const frequentContacts: PersonNeighbor[] = [...network.neighbors]
        .filter((neighbor) => neighbor.slug && (neighbor.canonical_name || neighbor.primary_email))
        .sort((a, b) => b.thread_count - a.thread_count || b.co_recipient_count - a.co_recipient_count)
        .slice(0, 6);

    const showDormancy =
        network.dormancy_days != null && network.dormancy_days > 90 && network.degree >= 5;
    const showRoleBadge = network.structural_role === "hub" || network.structural_role === "bridge";

    return (
        <div>
            <div className="border-b border-outline-variant pb-2">
                <h4 className="text-label-caps font-label-caps text-on-surface-variant">
                    OFTEN ON EMAILS TOGETHER
                </h4>
            </div>

            <div className="mt-4 space-y-4">
                {showRoleBadge && (
                    <span
                        title={ROLE_EXPLANATION[network.structural_role] ?? ""}
                        className={`flex w-fit shrink-0 mb-2 text-[11px] font-bold px-2 py-0.5 rounded ${roleBadgeClass(network.structural_role)}`}
                    >
            {ROLE_LABEL[network.structural_role] ?? network.structural_role}
          </span>
                )}

                {frequentContacts.length > 0 && (
                    <ul className="space-y-3">
                        {frequentContacts.map((n) => {
                            const avatar = gravatarUrl(n.primary_email, 48);
                            const dispName = displayContactName(n.canonical_name, n.primary_email);
                            return (
                                <li key={n.person_id}>
                                    <Link
                                        href={n.slug ? `/people/${n.slug}` : "#"}
                                        className="flex items-center justify-between gap-3 group hover:bg-surface-container-low rounded p-1 -m-1"
                                    >
                                        <div className="flex items-center gap-2 min-w-0">
                                            <div
                                                className="w-7 h-7 rounded-full bg-primary text-white flex items-center justify-center text-[10px] font-bold flex-shrink-0 overflow-hidden">
                                                {n.primary_email ? (
                                                    <img
                                                        src={avatar}
                                                        alt={dispName}
                                                        className="w-full h-full rounded-full object-cover"
                                                    />
                                                ) : (
                                                    contactInitials(dispName)
                                                )}
                                            </div>
                                            <span
                                                className="text-[12px] font-medium text-on-surface truncate group-hover:text-primary">
                        {dispName}
                      </span>
                                        </div>
                                        <span
                                            title={`${n.thread_count.toLocaleString()} shared threads`}
                                            className="text-[12px] font-bold text-on-surface flex-shrink-0 underline decoration-dotted decoration-on-surface-variant/40 underline-offset-4"
                                        >
                      {n.thread_count.toLocaleString()}
                    </span>
                                    </Link>
                                </li>
                            );
                        })}
                    </ul>
                )}

                {showDormancy && (
                    <p className="text-[12px] text-on-surface-variant">
                        Last contact:{" "}
                        <strong className="text-on-surface">{network.dormancy_days} days ago</strong>
                        {" "}— previously an active contact.
                    </p>
                )}
            </div>
        </div>
    );
}

// Local slugify duplicate — keep client-only file free of server imports.
function slugifyExternal(name: string): string {
    return name
        .toLowerCase()
        .normalize("NFKD")
        .replace(/[̀-ͯ]/g, "")
        .replace(/[^a-z0-9\s-]/g, "")
        .trim()
        .replace(/\s+/g, "-")
        .replace(/-+/g, "-");
}
