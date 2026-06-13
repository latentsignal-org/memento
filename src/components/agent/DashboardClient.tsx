"use client";
import {useState} from "react";
import Link from "next/link";
import {useRouter} from "next/navigation";
import {Bot, Search,} from "lucide-react";
import MentionInput from "@/components/agent/MentionInput";
import {contactInitials, displayContactName, maskEmailAddresses} from "@/lib/contact-display";
import {type ContextRef, encodeContextRefs} from "@/lib/context-refs";
import {formatMonth, formatMonthDay, relativeDate} from "@/lib/date-utils";

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

interface DashboardClientProps {
    initialData: DashboardModel;
}

function stripCitations(text: string) {
    return maskEmailAddresses(text.replace(/\s*\[msg:[^\]]+\]/g, "").replace(/\s+/g, " ").trim());
}

function compactText(text: string, maxLength: number) {
    const clean = stripCitations(text);
    if (clean.length <= maxLength) return clean;
    return `${clean.slice(0, maxLength).trimEnd()}...`;
}

function firstSentences(text: string, count: number) {
    const clean = stripCitations(text);
    const matches = clean.match(/[^.!?]+[.!?]+/g);
    if (!matches || matches.length === 0) return compactText(clean, 260);
    return matches.slice(0, count).join(" ").replace(/\s+/g, " ").trim();
}

function statusTone(project: ProjectDashboardItem) {
    switch (project.status) {
        case "blocked":
            return {
                label: "Blocked",
                className: "bg-tertiary-fixed text-on-tertiary-fixed border-tertiary/20",
                icon: "report"
            };
        case "completed":
            return {
                label: "Completed",
                className: "bg-primary-fixed text-on-primary-fixed border-primary/10",
                icon: "task_alt"
            };
        case "active":
            return {
                label: "Active",
                className: "bg-surface-container-high text-on-surface-variant border-outline-variant/40",
                icon: "radio_button_checked"
            };
        default:
            return {
                label: project.status || "Active",
                className: "bg-surface-container-high text-on-surface-variant border-outline-variant/40",
                icon: "radio_button_checked"
            };
    }
}

function toneClasses(tone: PriorityItem["tone"]) {
    switch (tone) {
        case "blocked":
            return "border-tertiary/30 bg-tertiary-fixed/35";
        case "success":
            return "border-primary/20 bg-primary-fixed/45";
        case "relationship":
            return "border-secondary-container bg-secondary-container/55";
        case "synthesis":
            return "border-outline-variant/60 bg-white";
    }
}

function ArchiveActivityCard({data}: { data: ArchiveActivityModel }) {
    const {years, totalMessages, firstYear, lastYear, peakYear, peakCount} = data;
    if (years.length === 0) {
        return null;
    }
    const maxCount = Math.max(...years.map((y) => y.count));
    const yearSpan = lastYear - firstYear + 1;
    return (
        <div className="bg-surface-container-low border border-outline-variant/50 rounded-lg p-6">
            <p className="text-label-caps font-label-caps text-primary">Archive Activity</p>
            <h2 className="text-ui-medium font-bold text-on-surface mt-1">Email volume by year</h2>
            <p className="text-ui-small text-on-surface-variant mt-1">
                {yearSpan}-year history · {totalMessages.toLocaleString()} messages
            </p>

            <div className="flex items-end gap-[3px] h-24 mt-4">
                {years.map((y) => {
                    const heightPct = maxCount ? (y.count / maxCount) * 100 : 0;
                    return (
                        <div
                            key={y.year}
                            className="flex-1 h-full flex flex-col justify-end"
                            title={`${y.year}: ${y.count.toLocaleString()}`}
                        >
                            <div
                                className={`w-full rounded-t-[3px] transition-all duration-300 ${
                                    y.year < 2006
                                        ? "bg-[#c4ebde]"
                                        : y.year < 2021
                                            ? "bg-[#2a4d44]"
                                            : "bg-[#12362e]"
                                }`}
                                style={{height: heightPct > 0 ? `max(2px, ${heightPct}%)` : "0px"}}
                            />
                        </div>
                    );
                })}
            </div>

            <div className="flex gap-[3px] mt-1 text-[9px] font-mono text-on-surface-variant">
                {years.map((y) => (
                    <span key={y.year} className="flex-1 text-center">
            {String(y.year).slice(-2)}
          </span>
                ))}
            </div>

            {peakYear > 0 && (
                <div
                    className="flex items-center justify-between text-ui-small text-on-surface-variant mt-3 pt-3 border-t border-outline-variant/40">
                    <p>
                        Peak: <span
                        className="font-bold text-primary">{peakCount.toLocaleString()} msgs</span> in {peakYear}
                    </p>
                    <div className="flex items-center gap-3 text-[10px] font-medium text-on-surface-variant">
                        <div className="flex items-center gap-1">
                            <span className="w-2.5 h-2.5 rounded-sm bg-[#c4ebde]"/>
                            <span>Earlier</span>
                        </div>
                        <div className="flex items-center gap-1">
                            <span className="w-2.5 h-2.5 rounded-sm bg-[#2a4d44]"/>
                            <span>Mid</span>
                        </div>
                        <div className="flex items-center gap-1">
                            <span className="w-2.5 h-2.5 rounded-sm bg-[#12362e]"/>
                            <span>Recent</span>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

export default function DashboardClient({initialData}: DashboardClientProps) {
    const router = useRouter();
    const [heroInput, setHeroInput] = useState("");
    const [heroContextRefs, setHeroContextRefs] = useState<ContextRef[]>([]);
    const {firstYear, lastYear, firstMessageAt, lastMessageAt, totalMessages} = initialData.archiveActivity;
    const archiveYearSpan = firstYear > 0 && lastYear > 0 ? lastYear - firstYear + 1 : 0;
    const archiveRange =
        firstMessageAt && lastMessageAt ? `${formatMonth(firstMessageAt)} – ${formatMonth(lastMessageAt)}` : "";

    const handleHeroSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        const query = heroInput.trim();
        if (!query) {
            router.push("/ask");
            return;
        }
        const params = new URLSearchParams({q: query});
        if (heroContextRefs.length > 0) {
            params.set("refs", encodeContextRefs(heroContextRefs));
        }
        router.push(`/ask?${params.toString()}`);
    };

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div className="w-full max-w-[1440px] mx-auto px-4 sm:px-6 py-8 sm:py-10 space-y-8">

                {/* Header Section */}
                <header className="flex items-start justify-between gap-4">
                    <div className="max-w-[820px]">
                        <h1 className="text-display-lg font-display-lg text-primary">Home</h1>
                        {(totalMessages > 0 || archiveYearSpan > 0) && (
                            <p className="mt-1 text-ui-small text-on-surface-variant">
                                {totalMessages > 0 && (
                                    <>
                                        <span
                                            className="font-bold text-on-surface">{totalMessages.toLocaleString()}</span> messages
                                    </>
                                )}
                                {totalMessages > 0 && archiveYearSpan > 0 && <span className="mx-2">•</span>}
                                {archiveYearSpan > 0 && (
                                    <>
                                        <span className="font-bold text-on-surface">{archiveYearSpan}</span> years
                                        archived
                                    </>
                                )}
                                {archiveRange &&
                                    <span className="ml-2 font-bold text-on-surface">({archiveRange})</span>}
                            </p>
                        )}
                    </div>
                    <div
                        className="inline-flex items-center gap-2 rounded-full border border-outline-variant/45 bg-surface-container px-3 py-1.5 text-[11px] font-medium text-on-surface-variant shadow-sm shrink-0 mt-1">
                        <span
                            className="font-mono text-[10px] uppercase tracking-[0.08em] text-primary font-bold">Generated</span>
                        {initialData.generatedAt ? formatMonthDay(initialData.generatedAt) : "from local exports"}
                    </div>
                </header>

                {/* Hero Chat Input (Glassmorphism & High Premium styling) */}
                <section className="relative my-7 sm:my-7">
                    <form
                        onSubmit={handleHeroSubmit}
                        className="flex items-center gap-2 sm:gap-3 rounded-xl border border-outline-variant/60 bg-surface-container-low px-2.5 py-2 sm:px-3.5 sm:py-4.5 shadow-md focus-within:ring-2 focus-within:ring-primary/20 transition-all max-w-[960px] mx-auto"
                    >
                        <div className="pl-1 sm:pl-3 flex items-center text-primary/75">
                            <Bot className="h-5 w-5 animate-pulse sm:h-6 sm:w-6"/>
                        </div>
                        <MentionInput
                            value={heroInput}
                            refs={heroContextRefs}
                            onChange={(nextValue, nextRefs) => {
                                setHeroInput(nextValue);
                                setHeroContextRefs(nextRefs);
                            }}
                            placeholder="Ask Memento. Use @people and #projects, #concepts, or #sessions for context..."
                            inputClassName="w-full min-w-0 bg-transparent text-sm sm:text-base text-on-surface focus:outline-none placeholder-on-surface-variant/60 py-3 sm:py-4 px-1"
                        />
                        <button
                            type="submit"
                            aria-label="Ask Memento"
                            className="flex items-center gap-2 rounded-lg bg-primary px-3 sm:px-4 py-3 text-sm font-semibold text-primary-foreground shadow-sm hover:opacity-90 active:scale-98 transition disabled:bg-primary-container disabled:text-primary-foreground disabled:opacity-100 disabled:shadow-none"
                        >
                            <Search className="h-4 w-4"/>
                            <span className="hidden sm:inline">Ask Memento</span>
                        </button>
                    </form>
                </section>

                <div className="mt-4 border-t border-outline-variant/40 pt-6">
                    {/* Default Executive Brief Dashboard */}
                    <div className="space-y-6">
                        {/* Main contents grids */}
                        <section className="grid grid-cols-1 lg:grid-cols-12 gap-6">
                            <div
                                className="lg:col-span-8 bg-surface-container-low border border-outline-variant/50 rounded-lg p-5 sm:p-6">
                                <div className="flex items-center justify-between gap-4 mb-5">
                                    <div>
                                        <p className="text-label-caps font-label-caps text-primary">Priority Brief</p>
                                        <h2 className="text-headline-md font-headline-md text-on-surface">What to look
                                            at first</h2>
                                    </div>
                                    <span className="text-ui-small text-on-surface-variant hidden sm:inline">Ranked from live dimension exports</span>
                                </div>

                                <div className="space-y-3">
                                    {initialData.priorityItems.map((item) => (
                                        <Link
                                            key={item.rank}
                                            href={item.href}
                                            className={`group flex gap-3 sm:gap-4 border rounded-lg p-4 transition-colors hover:bg-white ${toneClasses(item.tone)}`}
                                        >
                                            <span
                                                className="text-headline-md font-headline-md text-primary/45 leading-none w-7 sm:w-10 shrink-0">{item.rank}</span>
                                            <div className="min-w-0 flex-1">
                                                <div className="flex flex-wrap items-center gap-2 mb-1">
                          <span
                              className="text-[10px] uppercase tracking-[0.06em] font-bold bg-white/70 text-primary px-2 py-0.5 rounded border border-outline-variant/40">
                            {item.label}
                          </span>
                                                </div>
                                                <h3 className="text-ui-medium font-bold text-primary">{item.title}</h3>
                                                <p className="text-ui-small text-on-surface-variant leading-relaxed mt-1">{compactText(item.body, 260)}</p>
                                            </div>
                                            <span
                                                className="material-symbols-outlined text-primary/60 self-center text-[18px] opacity-70 group-hover:opacity-100">
                        arrow_forward
                      </span>
                                        </Link>
                                    ))}
                                </div>
                            </div>

                            <aside className="lg:col-span-4 space-y-6">
                                <div className="bg-primary text-primary-foreground rounded-lg p-6">
                                    <div className="flex items-center justify-between">
                                        <h2 className="text-ui-medium font-bold">Coverage Snapshot</h2>
                                        <span
                                            className="material-symbols-outlined text-primary-foreground/70 text-[20px]">hub</span>
                                    </div>
                                    <div className="grid grid-cols-2 gap-4 mt-5">
                                        <div>
                                            <p className="text-[10px] uppercase tracking-[0.08em] opacity-70">Relationships</p>
                                            <p className="text-headline-md font-headline-md">{initialData.metrics.people}</p>
                                        </div>
                                        <div>
                                            <p className="text-[10px] uppercase tracking-[0.08em] opacity-70">Project
                                                Emails</p>
                                            <p className="text-headline-md font-headline-md">{initialData.metrics.projectMessages}</p>
                                        </div>
                                        <div>
                                            <p className="text-[10px] uppercase tracking-[0.08em] opacity-70">Sources</p>
                                            <p className="text-headline-md font-headline-md">{initialData.metrics.newsletterSources}</p>
                                        </div>
                                        <div>
                                            <p className="text-[10px] uppercase tracking-[0.08em] opacity-70">Broadcast</p>
                                            <p className="text-headline-md font-headline-md">{initialData.metrics.newsletterEmails.toLocaleString()}</p>
                                        </div>
                                    </div>
                                </div>

                                <ArchiveActivityCard data={initialData.archiveActivity}/>

                            </aside>
                        </section>

                        <section className="grid grid-cols-1 xl:grid-cols-2 gap-6">
                            <div
                                className="bg-surface-container-low border border-outline-variant/50 rounded-lg p-5 sm:p-6">
                                <div className="flex items-center justify-between mb-5">
                                    <div>
                                        <p className="text-label-caps font-label-caps text-primary">Project Status</p>
                                        <h2 className="text-headline-md font-headline-md text-on-surface">Bounded
                                            narratives</h2>
                                    </div>
                                    <Link href="/projects"
                                          className="text-ui-small font-bold text-primary hover:underline">
                                        View all
                                    </Link>
                                </div>

                                <div className="space-y-5">
                                    {initialData.projectItems.slice(0, 3).map((project) => {
                                        const tone = statusTone(project);
                                        return (
                                            <Link
                                                key={project.slug}
                                                href={`/projects/${project.slug}`}
                                                className="block bg-white border border-outline-variant/45 rounded-lg p-4 hover:border-outline transition-colors"
                                            >
                                                <div className="flex flex-wrap items-center justify-between gap-3">
                                                    <div>
                                                        <h3 className="text-ui-medium font-bold text-primary">{project.name}</h3>
                                                        <p className="text-ui-small text-on-surface-variant mt-1">
                                                            {project.messageCount} messages
                                                            · {formatMonth(project.dateRange.first)} to {formatMonth(project.dateRange.last)}
                                                        </p>
                                                    </div>
                                                    <span
                                                        className={`inline-flex items-center gap-1 border rounded px-2 py-1 text-[10px] uppercase tracking-[0.06em] font-bold ${tone.className}`}>
                            <span className="material-symbols-outlined text-[14px]">{tone.icon}</span>
                                                        {tone.label}
                          </span>
                                                </div>
                                                <p className="text-ui-small text-on-surface-variant leading-relaxed mt-3">
                                                    {firstSentences(project.currentUnderstanding || project.summary, 1)}
                                                </p>
                                                <div className="flex flex-wrap gap-2 mt-4">
                                                    {project.members.slice(0, 4).map((member) => (
                                                        <span key={`${project.slug}-${member.canonical_name}`}
                                                              className="inline-flex items-center gap-1.5 bg-surface-container text-on-surface-variant px-2 py-1 rounded text-[10px] font-mono">
                              {member.avatar_url && (
                                  <img src={member.avatar_url} alt="" className="w-4 h-4 rounded-full object-cover"/>
                              )}
                                                            {displayContactName(member.canonical_name, member.primary_email)}
                            </span>
                                                    ))}
                                                </div>
                                            </Link>
                                        );
                                    })}
                                </div>
                            </div>

                            <div
                                className="bg-surface-container-low border border-outline-variant/50 rounded-lg p-5 sm:p-6">
                                <div className="flex items-center justify-between mb-5">
                                    <div>
                                        <p className="text-label-caps font-label-caps text-primary">Relationship
                                            Radar</p>
                                        <h2 className="text-headline-md font-headline-md text-on-surface">Recent active
                                            people</h2>
                                    </div>
                                    <Link href="/people"
                                          className="text-ui-small font-bold text-primary hover:underline">
                                        Open people
                                    </Link>
                                </div>

                                <div className="space-y-3">
                                    {initialData.peopleItems.map((person) => (
                                        <Link
                                            href="/people"
                                            key={person.email}
                                            className="flex gap-3 bg-white border border-outline-variant/45 rounded-lg p-3 hover:border-outline transition-colors"
                                        >
                                            <div
                                                className="w-10 h-10 rounded bg-primary-fixed text-on-primary-fixed flex items-center justify-center font-bold shrink-0 overflow-hidden">
                                                {person.avatarUrl ? (
                                                    <img src={person.avatarUrl} alt={person.name}
                                                         className="w-full h-full object-cover"/>
                                                ) : (
                                                    contactInitials(person.name)
                                                )}
                                            </div>
                                            <div className="min-w-0 flex-1">
                                                <h3 className="text-ui-medium font-bold text-primary">{person.name}</h3>
                                                <p className="text-ui-small text-on-surface-variant mt-1 line-clamp-1">
                                                    {person.totalMessages} messages
                                                    · {relativeDate(person.lastMessageAt)} · {person.latestSubject}
                                                </p>
                                            </div>
                                        </Link>
                                    ))}
                                </div>
                            </div>
                        </section>

                        <section>
                            <div
                                className="bg-surface-container-low border border-outline-variant/50 rounded-lg p-5 sm:p-6">
                                <div className="flex items-center justify-between mb-5">
                                    <div>
                                        <p className="text-label-caps font-label-caps text-primary">Newsletter Pulse</p>
                                        <h2 className="text-headline-md font-headline-md text-on-surface">Coverage and
                                            recency</h2>
                                    </div>
                                    <Link href="/newsletters"
                                          className="text-ui-small font-bold text-primary hover:underline">
                                        View sources
                                    </Link>
                                </div>

                                <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                                    <div>
                                        <h3 className="text-ui-small font-bold text-primary mb-3">Top by archive
                                            volume</h3>
                                        <div className="space-y-2">
                                            {initialData.newsletterItems.map((source) => (
                                                <Link
                                                    href={`/newsletters/${source.slug}`}
                                                    key={source.slug}
                                                    className="flex items-center justify-between gap-3 bg-white border border-outline-variant/45 rounded p-3 hover:border-outline"
                                                >
                                                    <span
                                                        className="text-ui-small font-bold text-on-surface truncate">{source.displayName}</span>
                                                    <span
                                                        className="font-mono text-[11px] text-primary font-bold">{source.messageCount}</span>
                                                </Link>
                                            ))}
                                        </div>
                                    </div>

                                    <div>
                                        <h3 className="text-ui-small font-bold text-primary mb-3">Most recent source
                                            activity</h3>
                                        <div className="space-y-2">
                                            {initialData.recentNewsletterItems.map((source) => (
                                                <Link
                                                    href={`/newsletters/${source.slug}`}
                                                    key={source.slug}
                                                    className="block bg-white border border-outline-variant/45 rounded p-3 hover:border-outline"
                                                >
                                                    <div className="flex items-center justify-between gap-3">
                                                        <span
                                                            className="text-ui-small font-bold text-on-surface truncate">{source.displayName}</span>
                                                        <span
                                                            className="text-[10px] text-on-surface-variant">{relativeDate(source.lastSeen)}</span>
                                                    </div>
                                                    <p className="text-[11px] text-on-surface-variant line-clamp-1 mt-1">{source.recentSubjects[0]}</p>
                                                </Link>
                                            ))}
                                        </div>
                                    </div>

                                    <div>
                                        <h3 className="text-ui-small font-bold text-primary mb-3">Top Newsletter
                                            Domains</h3>
                                        <div className="space-y-2">
                                            {initialData.topDomains.map((domain) => (
                                                <div
                                                    key={domain.domain}
                                                    className="flex items-center justify-between gap-3 bg-white border border-outline-variant/45 rounded p-3"
                                                >
                                                    <span
                                                        className="text-ui-small text-on-surface-variant truncate">{domain.domain}</span>
                                                    <span
                                                        className="font-mono text-[11px] text-primary font-bold">{domain.messages.toLocaleString()}</span>
                                                </div>
                                            ))}
                                        </div>
                                    </div>
                                </div>

                                {initialData.topNewsletter && initialData.topNewsletterSummary && (
                                    <Link href={`/newsletters/${initialData.topNewsletter.slug}`}
                                          className="block mt-6 bg-primary text-primary-foreground rounded-lg p-5 hover:bg-primary-container transition-colors">
                                        <div className="flex items-center justify-between gap-4">
                                            <div>
                                                <p className="text-[10px] uppercase tracking-[0.08em] opacity-75 font-bold">Generated
                                                    Synthesis</p>
                                                <h3 className="text-ui-medium font-bold mt-1">{initialData.topNewsletter.displayName} coverage
                                                    summary</h3>
                                            </div>
                                            <span
                                                className="material-symbols-outlined text-[18px] opacity-70">arrow_forward</span>
                                        </div>
                                        <p className="text-ui-small leading-relaxed opacity-90 mt-3">
                                            {firstSentences(initialData.topNewsletterSummary, 2)}
                                        </p>
                                    </Link>
                                )}
                            </div>
                        </section>
                    </div>
                </div>
            </div>
        </main>
    );
}
