"use client";

import {useState} from "react";
import Link from "next/link";
import {maskEmail, maskEmailAddresses} from "@/lib/contact-display";
import {formatMonth} from "@/lib/date-utils";
import {DeleteButton} from "@/components/DeleteButton";

interface NewsletterSource {
    slug: string;
    display_name: string;
    sender_email: string;
    domain: string;
    message_count: number;
    last_seen?: string;
    recent_subjects: string[];
}

export default function NewslettersClient({initialSources}: { initialSources: NewsletterSource[] }) {
    const [sources, setSources] = useState(initialSources);

    async function deleteNewsletter(slug: string) {
        await fetch(`/api/newsletters/${slug}`, {method: "DELETE"});
        setSources((prev) => prev.filter((s) => s.slug !== slug));
    }

    return (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            {sources.map((source) => (
                <Link
                    key={source.slug}
                    href={`/newsletters/${source.slug}`}
                    className="group relative flex flex-col justify-between p-6 bg-surface-container-low rounded-2xl border border-outline-variant/40 hover:border-outline-variant hover:bg-white transition-all duration-300 hover:-translate-y-1 hover:shadow-md"
                >
                    <div>
                        <div className="flex items-center justify-between mb-4">
              <span
                  className="bg-primary text-white px-2.5 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider">
                Source
              </span>
                            <div className="flex items-center gap-2">
                <span className="text-[11px] text-on-surface-variant font-medium">
                  {formatMonth(source.last_seen)}
                </span>
                                <DeleteButton onDelete={() => deleteNewsletter(source.slug)}/>
                            </div>
                        </div>

                        <h2 className="text-headline-md font-headline-md text-primary font-bold mb-2 group-hover:text-primary-container transition-colors">
                            {maskEmailAddresses(source.display_name)}
                        </h2>
                        <p className="text-ui-small text-on-surface-variant font-mono mb-4 truncate">
                            {maskEmail(source.sender_email)}
                        </p>

                        <div className="space-y-2 mb-6">
                            {source.recent_subjects.slice(0, 3).map((subject, idx) => (
                                <p key={`${idx}:${subject}`}
                                   className="text-ui-small text-on-surface-variant line-clamp-1">
                                    {maskEmailAddresses(subject)}
                                </p>
                            ))}
                        </div>
                    </div>

                    <div className="border-t border-outline-variant/40 pt-4 flex items-center justify-between">
                        <div className="flex items-center gap-2">
              <span
                  className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold">
                {source.message_count} messages
              </span>
                            <span
                                className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold">
                {source.domain}
              </span>
                        </div>
                        <span
                            className="material-symbols-outlined text-primary text-lg opacity-0 group-hover:opacity-100 transition-opacity">
              arrow_forward
            </span>
                    </div>
                </Link>
            ))}
        </div>
    );
}
