import {Contact} from "@/components/contact-detail";
import {displayContactName, maskEmail, maskEmailAddresses} from "@/lib/contact-display";
import {gravatarUrl} from "@/lib/gravatar";
import {formatMonthDay} from "@/lib/date-utils";
import {slugify} from "@/lib/citation";

const GENERIC_DOMAINS = new Set([
    "gmail.com", "yahoo.com", "hotmail.com", "outlook.com",
    "aol.com", "icloud.com", "txt.voice.google.com",
]);

export function orgFromDomain(domain: string): string {
    if (!domain) return "External";
    if (GENERIC_DOMAINS.has(domain.toLowerCase())) return "Personal";
    const name = domain.split(".")[0];
    return name.charAt(0).toUpperCase() + name.slice(1);
}

function classificationToStatus(c: string): Contact["status"] {
    switch (c) {
        case "candidate":
            return "ACTIVE";
        case "weak_signal":
            return "PROPOSED";
        case "candidate_inbound_only":
            return "DORMANT";
        case "excluded":
            return "EXCLUDED";
        default:
            return "DORMANT";
    }
}

export function mapPersonToContact(person: any, generatedAtStr?: string, privacyEnabled?: boolean): Contact {
    const displayName = displayContactName(person.canonical_name, person.primary_email, privacyEnabled);
    const organization = orgFromDomain(person.domain || "");
    const status = classificationToStatus(person.classification);

    const lastDate = person.last_message_at ? new Date(person.last_message_at) : null;
    const lastInteraction = lastDate ? formatMonthDay(person.last_message_at) : "Unknown";

    const relativeNow = new Date();
    let reengagementText = "";
    if (lastDate) {
        const diffDays = Math.round((relativeNow.getTime() - lastDate.getTime()) / (1000 * 60 * 60 * 24));
        if (diffDays > 30) {
            reengagementText = maskEmailAddresses(
                `${displayName} and you last corresponded ${diffDays} days ago. Consider checking in on their last email: "${person.timeline?.[0]?.subject || "No subject"}".`,
                privacyEnabled
            );
        }
    }

    const firstDateStr = person.first_message_at ? formatMonthDay(person.first_message_at) : "unknown date";
    const lastDateStr = person.last_message_at ? formatMonthDay(person.last_message_at) : "unknown date";
    const monthsSpan =
        person.first_message_at && person.last_message_at
            ? Math.max(1, Math.round(
                (new Date(person.last_message_at).getTime() - new Date(person.first_message_at).getTime()) /
                (1000 * 60 * 60 * 24 * 30.5)
            ))
            : 0;

    const narrativeHtml = `
    <p><strong>${displayName}</strong> is a correspondence contact with <strong>${person.total_messages || 0}</strong> total messages in the archive, including <strong>${person.from_contact_count || 0}</strong> inbound messages and <strong>${person.to_contact_count || 0}</strong> outbound messages.</p>
    <p>Active communication in the archive spans ${monthsSpan ? `approximately <strong>${monthsSpan}</strong> months` : "a brief period"}, from the first recorded contact on ${firstDateStr} to the most recent on ${lastDateStr}.</p>
    <p>All communication occurred via the primary resolved address <code>${maskEmail(person.primary_email, privacyEnabled)}</code> across <strong>${person.aliases ? person.aliases.length : 0}</strong> identified aliases.</p>
  `;

    const topics: Contact["topics"] = [];
    if (person.exclusion_reason) {
        topics.push({title: "Excluded by classifier", count: person.exclusion_reason});
    }
    if (person.domain) {
        topics.push({
            title: `Domain Communications (${person.domain})`,
            count: `Covers ${person.email_count || 0} unique sender/recipient addresses`
        });
    }
    if (person.top_correspondents?.length > 0) {
        const topCorr = person.top_correspondents[0];
        topics.push({
            title: `Shared Context with ${displayContactName(topCorr.canonical_name, topCorr.primary_email, privacyEnabled)}`,
            count: `Shared ${topCorr.shared_count} messages in mutual threads`
        });
    }
    if (topics.length === 0) {
        topics.push({title: "Email Correspondence", count: `${person.total_messages} messages total`});
    }

    const timeline: Contact["timeline"] = (person.timeline || []).map((item: any) => ({
        messageId: item.message_id,
        date: item.date ? formatMonthDay(item.date) : "Unknown date",
        subject: maskEmailAddresses(item.subject || "(No Subject)", privacyEnabled),
        snippet: maskEmailAddresses(item.snippet || "", privacyEnabled),
        direction: item.direction || "from_contact",
        viaEmail: item.via_email || "",
    }));

    const mutualContacts = person.top_correspondents
        ? person.top_correspondents.slice(0, 3).map((c: any) => gravatarUrl(c.primary_email, 48))
        : [];
    const mutualContactsCount = person.top_correspondents
        ? Math.max(0, person.top_correspondents.length - 3)
        : 0;

    return {
        id: String(person.person_id),
        slug: person.slug || slugify(person.canonical_name),
        name: displayName,
        primaryEmail: person.primary_email || "",
        domain: person.domain || "",
        role: "Correspondent",
        organization,
        status,
        lastInteraction,
        lastInteractionTime: person.last_message_at || "",
        avatarUrl: gravatarUrl(person.primary_email, 128),
        alias: person.aliases
            ? person.aliases.map((a: any) => `${a.display_name || "Alias"} · ${maskEmail(a.email_address, privacyEnabled)}`)
            : [],
        sourcesCount: person.total_messages || 0,
        lastUpdated: generatedAtStr || "local export",
        narrativeHtml,
        reengagementText,
        topics,
        mutualContacts,
        mutualContactsCount,
        threads: [],
        timeline,
        structuralRole: person.structural_role || "",
        clusterLabel: person.cluster_label || "",
        clusterId: person.cluster_id ?? null,
        dormancyDays: person.dormancy_days ?? null,
    };
}
