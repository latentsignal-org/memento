"use client";

import {Sheet, SheetContent} from "@/components/ui/sheet";
import {Avatar, AvatarFallback, AvatarImage} from "@/components/ui/avatar";
import {displayContactName, maskEmailAddresses} from "@/lib/contact-display";
import EmailReveal from "@/components/email-reveal";

export interface Thread {
    title: string;
    snippet: string;
    status: "WAITING ON YOU" | "DORMANT";
}

export interface TimelineItem {
    messageId: number;
    date: string;
    subject: string;
    snippet: string;
    direction: "from_contact" | "to_contact";
    viaEmail: string;
}

export interface Topic {
    title: string;
    count: string;
}

// Structured relationship-narrative data. Deliberately not raw HTML: the
// display name is derived from untrusted email participant data, so rendering
// it through dangerouslySetInnerHTML would be a stored-injection sink. The
// component composes these primitives as JSX, which React escapes.
export interface ContactNarrative {
    displayName: string;
    totalMessages: number;
    inbound: number;
    outbound: number;
    monthsSpan: number; // 0 => "a brief period"
    firstDate: string;
    lastDate: string;
    maskedEmail: string;
    aliasCount: number;
}

export interface Contact {
    id: string;
    slug: string; // url-safe key for /people/[slug]
    name: string;
    primaryEmail: string;
    domain: string;
    role: string;
    organization: string;
    status: "ACTIVE" | "PROPOSED" | "DORMANT" | "EXCLUDED";
    lastInteraction: string;
    lastInteractionTime: string;
    avatarUrl: string;
    alias: string[];
    sourcesCount: number;
    lastUpdated: string;
    narrative: ContactNarrative;
    reengagementText: string;
    topics: Topic[];
    mutualContacts: string[]; // URLs
    mutualContactsCount: number;
    threads: Thread[];
    timeline: TimelineItem[];
    structuralRole: string;
    clusterLabel: string;
    clusterId: number | null;
    dormancyDays: number | null;
}

interface ContactDetailProps {
    contact: Contact | null;
    isOpen: boolean;
    onClose: () => void;
}

export default function ContactDetail({contact, isOpen, onClose}: ContactDetailProps) {
    if (!contact) return null;
    const displayName = displayContactName(contact.name, contact.primaryEmail);

    // Render badge class depending on status
    const getStatusBadge = (status: string) => {
        switch (status) {
            case "ACTIVE":
                return "bg-primary text-white text-[10px] font-bold tracking-wider px-2 py-0.5 rounded";
            case "PROPOSED":
                return "border border-dashed border-outline-variant text-on-surface-variant text-[10px] font-bold tracking-wider px-2 py-0.5 rounded";
            case "DORMANT":
                return "bg-surface-container-highest text-on-surface-variant text-[10px] font-bold tracking-wider px-2 py-0.5 rounded";
            case "EXCLUDED":
                return "border border-outline-variant text-on-surface-variant text-[10px] font-bold tracking-wider px-2 py-0.5 rounded";
            default:
                return "";
        }
    };

    return (
        <Sheet open={isOpen} onOpenChange={(open) => !open && onClose()}>
            <SheetContent
                side="right"
                className="w-full sm:max-w-2xl md:max-w-4xl overflow-y-auto bg-background text-on-surface border-l border-outline-variant p-8 focus:outline-none"
            >
                {/* Contact Header */}
                <section className="mb-12 border-b border-outline-variant pb-8 mt-4">
                    <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-4">
                        <div className="flex items-center gap-4">
                            <Avatar className="w-16 h-16 rounded-xl border border-outline-variant">
                                <AvatarImage src={contact.avatarUrl} alt={displayName}/>
                                <AvatarFallback>{displayName.charAt(0)}</AvatarFallback>
                            </Avatar>
                            <div>
                                <div className="flex items-center gap-2 mb-1">
                                    <h1 className="text-[32px] font-semibold font-display-lg text-primary tracking-tight leading-none">
                                        {displayName}
                                    </h1>
                                    <span className={getStatusBadge(contact.status)}>{contact.status}</span>
                                </div>

                                <p className="text-ui-small font-ui-small text-on-surface-variant mt-1.5 flex items-center gap-1.5">
                                    <span className="material-symbols-outlined text-[16px] opacity-75">mail</span>
                                    <EmailReveal email={contact.primaryEmail} className="text-primary"/>
                                </p>
                                <div className="flex flex-wrap gap-2 mt-3">
                                    {contact.alias.map((aliasStr, idx) => (
                                        <span
                                            key={idx}
                                            className="text-[11px] text-on-surface-variant px-2 py-0.5 bg-surface-container-highest rounded border border-outline-variant"
                                        >
                      {aliasStr}
                    </span>
                                    ))}
                                </div>
                            </div>
                        </div>
                        <div className="text-left sm:text-right flex-shrink-0">
                            <div
                                className="inline-flex items-center gap-2 px-3 py-1 bg-surface-container-low border border-outline-variant rounded mb-2">
                                <span className="material-symbols-outlined text-[14px] text-primary">verified</span>
                                <span
                                    className="text-label-caps text-on-surface-variant">{contact.sourcesCount} SOURCES</span>
                            </div>
                            <p className="text-ui-small text-on-surface-variant opacity-60">Last
                                updated {contact.lastUpdated}</p>
                        </div>
                    </div>
                </section>

                {/* Grid Layout for details */}
                <div className="grid grid-cols-1 md:grid-cols-3 gap-12">
                    {/* Left / Center: Narrative & Topics */}
                    <div className="md:col-span-2 space-y-12">
                        <article className="prose prose-stone prose-lg max-w-none">
                            <h3 className="text-label-caps text-on-surface-variant mb-4 pb-2 border-b border-outline-variant">
                                Relationship Narrative
                            </h3>
                            <div className="text-body-reading font-body-reading text-on-surface leading-relaxed mb-6">
                                <p>
                                    <strong>{contact.narrative.displayName}</strong> is a correspondence contact
                                    with <strong>{contact.narrative.totalMessages}</strong> total messages in the
                                    archive, including <strong>{contact.narrative.inbound}</strong> inbound messages
                                    and <strong>{contact.narrative.outbound}</strong> outbound messages.
                                </p>
                                <p>
                                    Active communication in the archive spans{" "}
                                    {contact.narrative.monthsSpan ? (
                                        <>approximately <strong>{contact.narrative.monthsSpan}</strong> months</>
                                    ) : (
                                        "a brief period"
                                    )}
                                    , from the first recorded contact on {contact.narrative.firstDate} to the most
                                    recent on {contact.narrative.lastDate}.
                                </p>
                                <p>
                                    All communication occurred via the primary resolved
                                    address <code>{contact.narrative.maskedEmail}</code> across{" "}
                                    <strong>{contact.narrative.aliasCount}</strong> identified aliases.
                                </p>
                            </div>
                        </article>

                        {/* Re-engagement Suggestion */}
                        {contact.reengagementText && (
                            <section className="bg-primary-container/5 border border-primary/10 p-6 rounded-xl">
                                <div className="flex items-center gap-3 mb-3">
                                    <span className="material-symbols-outlined text-primary">auto_awesome</span>
                                    <h3 className="text-ui-medium font-bold text-primary font-ui-medium">Re-engagement
                                        Suggestion</h3>
                                </div>
                                <p className="text-ui-medium text-on-surface-variant mb-4">
                                    {contact.reengagementText}
                                </p>
                                <div className="flex gap-3">
                                    <button
                                        className="bg-primary text-on-primary px-5 py-1.5 rounded-full text-ui-small font-bold hover:opacity-90 transition-opacity">
                                        Draft Follow-up
                                    </button>
                                    <button
                                        className="border border-outline-variant text-on-surface-variant px-5 py-1.5 rounded-full text-ui-small font-bold hover:bg-surface-container-highest transition-colors">
                                        Dismiss
                                    </button>
                                </div>
                            </section>
                        )}

                        {/* Recurring Topics */}
                        <section>
                            <h3 className="text-label-caps text-on-surface-variant mb-4 pb-2 border-b border-outline-variant">
                                Recurring Topics
                            </h3>
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                                {contact.topics.map((topic, idx) => (
                                    <div
                                        key={idx}
                                        className="p-4 bg-surface-container-low border border-outline-variant rounded-lg hover:border-primary transition-colors cursor-default group"
                                    >
                                        <p className="text-ui-medium font-bold text-primary group-hover:underline">
                                            {topic.title}
                                        </p>
                                        <p className="text-ui-small text-on-surface-variant">{topic.count}</p>
                                    </div>
                                ))}
                            </div>
                        </section>
                    </div>

                    {/* Right Column: Vitals, Open Threads, Timeline */}
                    <aside className="space-y-10">
                        {/* Quick Vitals */}
                        <div>
                            <h4 className="text-label-caps text-on-surface-variant mb-4 pb-2 border-b border-outline-variant">
                                VITALS
                            </h4>
                            <ul className="space-y-4">
                                <li className="flex justify-between items-start">
                                    <span className="text-ui-small text-on-surface-variant">Last Contact</span>
                                    <span className="text-ui-small font-bold text-on-surface text-right">
                    {contact.lastInteraction}
                  </span>
                                </li>
                                <li className="flex justify-between items-center">
                                    <span className="text-ui-small text-on-surface-variant">Mutual Contacts</span>
                                    <div className="flex -space-x-2">
                                        {contact.mutualContacts.map((url, idx) => (
                                            <Avatar key={idx} className="w-6 h-6 border border-background">
                                                <AvatarImage src={url}/>
                                                <AvatarFallback>?</AvatarFallback>
                                            </Avatar>
                                        ))}
                                        {contact.mutualContactsCount > 0 && (
                                            <div
                                                className="w-6 h-6 rounded-full bg-surface-container-highest border border-background flex items-center justify-center text-[8px] font-bold">
                                                +{contact.mutualContactsCount}
                                            </div>
                                        )}
                                    </div>
                                </li>
                            </ul>
                        </div>

                        {/* Open Threads */}
                        <div>
                            <h4 className="text-label-caps text-on-surface-variant mb-4 pb-2 border-b border-outline-variant">
                                OPEN THREADS
                            </h4>
                            <div className="space-y-3">
                                {contact.threads.map((thread, idx) => (
                                    <div
                                        key={idx}
                                        className="p-3 bg-surface border border-outline-variant rounded hover:shadow-sm transition-shadow group cursor-pointer"
                                    >
                                        <p className="text-ui-small font-bold text-primary mb-1">{thread.title}</p>
                                        <p className="text-[10px] text-on-surface-variant line-clamp-1">
                                            {thread.snippet}
                                        </p>
                                        <div className="mt-2 flex items-center gap-2">
                      <span
                          className={`w-2 h-2 rounded-full ${
                              thread.status === "WAITING ON YOU" ? "bg-primary animate-pulse" : "bg-surface-variant"
                          }`}
                      ></span>
                                            <span
                                                className="text-[10px] text-on-surface-variant uppercase tracking-tighter">
                        {thread.status}
                      </span>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>

                        {/* Timeline */}
                        <div>
                            <h4 className="text-label-caps text-on-surface-variant mb-6 pb-2 border-b border-outline-variant">
                                TIMELINE
                            </h4>
                            <div
                                className="relative pl-4 space-y-6 before:content-[''] before:absolute before:left-0 before:top-0 before:bottom-0 before:w-px before:bg-outline-variant">
                                {contact.timeline.map((item, idx) => {
                                    const isOutbound = item.direction === "to_contact";
                                    return (
                                        <div key={idx} className="relative">
                      <span
                          className={`absolute -left-[21px] top-1.5 w-2 h-2 rounded-full ring-4 ring-background ${
                              idx === 0 ? "bg-primary animate-pulse" : "bg-outline"
                          }`}
                      ></span>
                                            <div className="flex flex-col gap-1.5">
                                                <div className="flex items-center gap-2 flex-wrap">
                          <span
                              className={`inline-flex items-center px-1.5 py-0.5 rounded text-[8px] font-bold uppercase tracking-wider ${
                                  isOutbound
                                      ? "bg-surface-container-highest text-on-surface-variant border border-outline-variant"
                                      : "bg-primary-container/20 text-primary border border-primary/20"
                              }`}>
                            {isOutbound ? "Outbound" : "Inbound"}
                          </span>
                                                    <span className="text-[10px] text-on-surface-variant font-mono">
                            ID: #{item.messageId}
                          </span>
                                                </div>
                                                <p className="text-ui-small font-bold text-on-surface leading-snug">
                                                    {maskEmailAddresses(item.subject)}
                                                </p>
                                                {item.snippet && (
                                                    <blockquote
                                                        className="border-l-2 border-outline-variant pl-2.5 py-0.5 my-1 text-ui-small text-on-surface-variant italic line-clamp-3 bg-surface-container-lowest/30 rounded-r">
                                                        {maskEmailAddresses(item.snippet)}
                                                    </blockquote>
                                                )}
                                                <p className="text-[10px] text-on-surface-variant opacity-75">
                                                    {item.date} {item.viaEmail && (
                                                    <>
                                                        {" "}via <EmailReveal email={item.viaEmail}/>
                                                    </>
                                                )}
                                                </p>
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        </div>
                    </aside>
                </div>
            </SheetContent>
        </Sheet>
    );
}
