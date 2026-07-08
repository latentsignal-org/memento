"use client";

import {useEffect, useState} from "react";
import {displayContactName, maskEmail, maskEmailAddresses} from "@/lib/contact-display";
import {avatarUrl, initialsFromName} from "@/lib/avatar";
import {apiGet, privacyEnabled as readPrivacyEnabled} from "@/lib/api";
import DashboardClient from "@/components/agent/DashboardClient";
import {relativeDate} from "@/lib/date-utils";

interface PersonDashboardItem {
    name: string;
    email: string;
    avatarUrl: string;
    totalMessages: number;
    lastMessageAt: string;
    latestSubject: string;
    latestSnippet: string;
    classification: string;
}

interface ProjectDashboardItem {
    name: string;
    slug: string;
    status: string;
    messageCount: number;
    dateRange: { first: string; last: string };
    members: { canonical_name: string; role: string; primary_email?: string; avatar_url?: string }[];
    summary: string;
    currentUnderstanding: string;
    frictionCount: number;
}

interface ConceptDashboardItem {
    name: string;
    slug: string;
    scopeDescription: string;
    status: string;
    messageCount: number;
    hasNarrative: boolean;
}

interface NewsletterSourceItem {
    displayName: string;
    slug: string;
    senderEmail: string;
    domain: string;
    messageCount: number;
    lastSeen: string;
    recentSubjects: string[];
}

interface PriorityItem {
    rank: string;
    label: string;
    title: string;
    body: string;
    href: string;
    tone: "blocked" | "success" | "relationship" | "synthesis";
}

interface ArchiveActivityModel {
    years: { year: number; count: number }[];
    totalMessages: number;
    firstYear: number;
    lastYear: number;
    firstMessageAt: string;
    lastMessageAt: string;
    peakYear: number;
    peakCount: number;
}

interface DashboardModel {
    metrics: {
        people: number;
        activeProjects: number;
        newsletterSources: number;
        newsletterEmails: number;
        projectMessages: number;
    };
    priorityItems: PriorityItem[];
    projectItems: ProjectDashboardItem[];
    peopleItems: PersonDashboardItem[];
    newsletterItems: NewsletterSourceItem[];
    recentNewsletterItems: NewsletterSourceItem[];
    topDomains: { domain: string; count: number; messages: number }[];
    topNewsletter: NewsletterSourceItem | null;
    topNewsletterSummary: string;
    archiveActivity: ArchiveActivityModel;
    generatedAt: string;
}

interface ProjectDetailResponse {
    name?: string;
    slug?: string;
    status?: string;
    message_count?: number;
    date_range?: { first: string; last: string };
    members?: { canonical_name: string; role: string; primary_email?: string; avatar_url?: string }[];
    narrative?: {
        summary?: string;
        current_understanding?: string;
        friction_points?: unknown[];
    };
}

interface PeopleIndexResponse {
    generated_at?: string;
    people?: {
        canonical_name?: string;
        primary_email?: string;
        total_messages?: number;
        last_message_at?: string;
        timeline?: { subject?: string; snippet?: string }[];
        classification?: string;
    }[];
}

interface NewsletterIndexResponse {
    sources?: {
        display_name?: string;
        slug?: string;
        sender_email?: string;
        domain?: string;
        message_count?: number;
        last_seen?: string;
        recent_subjects?: string[];
    }[];
}

interface ConceptIndexResponse {
    concepts?: {
        name?: string;
        slug?: string;
        scope_description?: string;
        status?: string;
        message_count?: number;
        has_narrative?: boolean;
    }[];
}

interface ArchiveActivityResponse {
    years?: { year: number; count: number }[];
    total_messages?: number;
    first_year?: number;
    last_year?: number;
    first_message_at?: string;
    last_message_at?: string;
    peak_year?: number;
    peak_count?: number;
}

interface NewsletterDetailResponse {
    narrative?: { coverage_summary?: string };
}

async function readApi<T>(path: string, fallback: T): Promise<T> {
    const data = await apiGet<T>(path);
    return data ?? fallback;
}

function stripCitations(text: string, privacyEnabled: boolean) {
    return maskEmailAddresses(text.replace(/\s*\[msg:[^\]]+\]/g, "").replace(/\s+/g, " ").trim(), privacyEnabled);
}

function compactText(text: string, maxLength: number, privacyEnabled: boolean) {
    const clean = stripCitations(text, privacyEnabled);
    if (clean.length <= maxLength) return clean;
    return `${clean.slice(0, maxLength).trimEnd()}...`;
}

function firstSentences(text: string, count: number, privacyEnabled: boolean) {
    const clean = stripCitations(text, privacyEnabled);
    const matches = clean.match(/[^.!?]+[.!?]+/g);
    if (!matches || matches.length === 0) return compactText(clean, 260, privacyEnabled);
    return matches.slice(0, count).join(" ").replace(/\s+/g, " ").trim();
}

async function loadProjects(): Promise<ProjectDashboardItem[]> {
    const index = await readApi<{ projects?: Array<{ slug: string }> }>("/api/projects", {projects: []});
    const slugs = (index.projects || []).map((p) => p.slug);
    const projects = await Promise.all(
        slugs.map(async (slug) => {
            const data = await readApi<ProjectDetailResponse>(`/api/projects/${slug}`, {});
            return {
                name: data.name || slug,
                slug: data.slug || slug,
                status: data.status || "active",
                messageCount: data.message_count || 0,
                dateRange: data.date_range || {first: "", last: ""},
                members: (data.members || []).map((member) => ({
                    ...member,
                    avatar_url: avatarUrl(member.primary_email, 48, initialsFromName(member.canonical_name, member.primary_email)),
                })),
                summary: data.narrative?.summary || "",
                currentUnderstanding: data.narrative?.current_understanding || "",
                frictionCount: data.narrative?.friction_points?.length || 0,
            } satisfies ProjectDashboardItem;
        })
    );
    return projects.sort((a, b) => b.dateRange.last.localeCompare(a.dateRange.last));
}

async function loadPeople(privacyEnabled: boolean): Promise<{ generatedAt: string; people: PersonDashboardItem[] }> {
    const data = await readApi<PeopleIndexResponse>("/api/people?top=50", {generated_at: "", people: []});
    const people = (data.people || []).map((person) => ({
        name: displayContactName(person.canonical_name, person.primary_email, privacyEnabled),
        email: person.primary_email || "",
        avatarUrl: avatarUrl(person.primary_email, 64, initialsFromName(person.canonical_name, person.primary_email)),
        totalMessages: person.total_messages || 0,
        lastMessageAt: person.last_message_at || "",
        latestSubject: maskEmailAddresses(person.timeline?.[0]?.subject || "No recent subject", privacyEnabled),
        latestSnippet: maskEmailAddresses(person.timeline?.[0]?.snippet || "", privacyEnabled),
        classification: person.classification || "",
    }));
    return {generatedAt: data.generated_at || "", people};
}

async function loadNewsletterSources(): Promise<NewsletterSourceItem[]> {
    const data = await readApi<NewsletterIndexResponse>("/api/newsletters", {sources: []});
    return (data.sources || []).map((source) => ({
        displayName: source.display_name || "Unknown source",
        slug: source.slug || "",
        senderEmail: source.sender_email || "",
        domain: source.domain || "",
        messageCount: source.message_count || 0,
        lastSeen: source.last_seen || "",
        recentSubjects: source.recent_subjects || [],
    }));
}

async function loadConcepts(): Promise<ConceptDashboardItem[]> {
    const data = await readApi<ConceptIndexResponse>("/api/concepts", {concepts: []});
    return (data.concepts || [])
        .map((concept): ConceptDashboardItem => ({
            name: concept.name || concept.slug || "Unnamed concept",
            slug: concept.slug || "",
            scopeDescription: concept.scope_description || "",
            status: concept.status || "active",
            messageCount: concept.message_count || 0,
            hasNarrative: Boolean(concept.has_narrative),
        }))
        .filter((concept: ConceptDashboardItem) => Boolean(concept.slug))
        .sort((a, b) => b.messageCount - a.messageCount);
}

async function loadArchiveActivity(): Promise<ArchiveActivityModel> {
    const data = await readApi<ArchiveActivityResponse>("/api/dashboard/activity", {
        years: [],
        total_messages: 0,
        first_year: 0,
        last_year: 0,
        first_message_at: "",
        last_message_at: "",
        peak_year: 0,
        peak_count: 0,
    });
    return {
        years: Array.isArray(data.years) ? data.years : [],
        totalMessages: data.total_messages || 0,
        firstYear: data.first_year || 0,
        lastYear: data.last_year || 0,
        firstMessageAt: data.first_message_at || "",
        lastMessageAt: data.last_message_at || "",
        peakYear: data.peak_year || 0,
        peakCount: data.peak_count || 0,
    };
}

async function loadTopNewsletterSummary(slug: string) {
    const data = await readApi<NewsletterDetailResponse>(`/api/newsletters/${slug}`, {});
    return data.narrative?.coverage_summary || "";
}

async function getDashboardModel(privacyEnabled: boolean): Promise<DashboardModel> {
    const [projects, concepts, peopleExport, newsletterSources, archiveActivity] = await Promise.all([
        loadProjects(),
        loadConcepts(),
        loadPeople(privacyEnabled),
        loadNewsletterSources(),
        loadArchiveActivity(),
    ]);

    const peopleByMessages = [...peopleExport.people].sort((a, b) => b.totalMessages - a.totalMessages);
    const peopleByRecent = [...peopleExport.people].sort((a, b) => b.lastMessageAt.localeCompare(a.lastMessageAt));
    const newslettersByCount = [...newsletterSources].sort((a, b) => b.messageCount - a.messageCount);
    const newslettersByRecent = [...newsletterSources].sort((a, b) => b.lastSeen.localeCompare(a.lastSeen));
    const topDomains = Object.values(
        newsletterSources.reduce<Record<string, { domain: string; count: number; messages: number }>>((acc, source) => {
            if (!source.domain) return acc;
            acc[source.domain] ||= {domain: source.domain, count: 0, messages: 0};
            acc[source.domain].count += 1;
            acc[source.domain].messages += source.messageCount;
            return acc;
        }, {})
    )
        .sort((a, b) => b.messages - a.messages)
        .slice(0, 6);

    const topNewsletter = newslettersByCount[0] ?? null;
    const topNewsletterSummary = topNewsletter ? await loadTopNewsletterSummary(topNewsletter.slug) : "";

    const topPerson = peopleByMessages[0] ?? null;
    const topProject = projects[0] ?? null;
    const topConcept = concepts[0] ?? null;

    const prioritySeed = [
        topProject && {
            label: topProject.status === "blocked" ? "Project blocked" : topProject.status === "completed" ? "Project completed" : "Project active",
            title: topProject.name,
            body: firstSentences(topProject.currentUnderstanding || topProject.summary, 1, privacyEnabled),
            href: `/projects/${topProject.slug}`,
            tone: (topProject.status === "blocked" ? "blocked" : "success") as PriorityItem["tone"],
        },
        topConcept && {
            label: topConcept.status === "active" ? "Concept active" : `Concept ${topConcept.status}`,
            title: topConcept.name,
            body:
                firstSentences(topConcept.scopeDescription, 1, privacyEnabled) ||
                `${topConcept.messageCount.toLocaleString()} associated messages${topConcept.hasNarrative ? " with narrative" : ""}.`,
            href: `/concepts/${topConcept.slug}`,
            tone: "success" as const,
        },
        topPerson && {
            label: "Relationship signal",
            title: `${topPerson.name} is the most active relationship in the archive`,
            body: `${topPerson.totalMessages.toLocaleString()} messages, most recently ${relativeDate(topPerson.lastMessageAt)}: ${topPerson.latestSubject}.`,
            href: "/people",
            tone: "relationship" as const,
        },
        topNewsletter && {
            label: "Newsletter synthesis",
            title: `${topNewsletter.displayName} — top newsletter by volume`,
            body: firstSentences(topNewsletterSummary, 1, privacyEnabled) || `${topNewsletter.messageCount.toLocaleString()} messages from ${maskEmail(topNewsletter.senderEmail, privacyEnabled)}.`,
            href: `/newsletters/${topNewsletter.slug}`,
            tone: "synthesis" as const,
        },
    ].filter(Boolean) as Omit<PriorityItem, "rank">[];

    const priorityItems: PriorityItem[] = prioritySeed.map((item, index) => ({
        ...item,
        rank: String(index + 1).padStart(2, "0"),
    }));

    return {
        metrics: {
            people: peopleExport.people.length,
            activeProjects: projects.filter((project) => project.status === "active").length,
            newsletterSources: newsletterSources.length,
            newsletterEmails: newsletterSources.reduce((sum, source) => sum + source.messageCount, 0),
            projectMessages: projects.reduce((sum, project) => sum + project.messageCount, 0),
        },
        priorityItems,
        projectItems: projects,
        peopleItems: peopleByRecent.slice(0, 5),
        newsletterItems: newslettersByCount.slice(0, 5),
        recentNewsletterItems: newslettersByRecent.slice(0, 5),
        topDomains,
        topNewsletter,
        topNewsletterSummary,
        archiveActivity,
        generatedAt: peopleExport.generatedAt,
    };
}

export default function DashboardPageClient() {
    const [dashboard, setDashboard] = useState<DashboardModel | null>(null);

    useEffect(() => {
        let cancelled = false;
        getDashboardModel(readPrivacyEnabled()).then((model) => {
            if (!cancelled) setDashboard(model);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (dashboard === null) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
                <span className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    return <DashboardClient initialData={dashboard}/>;
}
