"use client";

import {useEffect, useState} from "react";
import PeopleDirectoryClient from "./PeopleDirectoryClient";
import {Contact} from "@/components/contact-detail";
import {mapPersonToContact} from "@/lib/people-mapping";
import {apiGet, privacyEnabled} from "@/lib/api";
import {formatMonthDay} from "@/lib/date-utils";

type UICounts = { ACTIVE: number; PROPOSED: number; DORMANT: number; EXCLUDED: number };
const DORMANT_DAYS = 90;
const EMPTY_COUNTS: UICounts = {ACTIVE: 0, PROPOSED: 0, DORMANT: 0, EXCLUDED: 0};

async function getPeopleData(privacy: boolean): Promise<{ contacts: Contact[]; counts: UICounts }> {
    try {
        const data = await apiGet<{
            people?: any[];
            generated_at?: string;
            counts?: Record<string, number>;
        }>("/api/people?top=200&classification=active");
        if (!data) return {contacts: [], counts: EMPTY_COUNTS};

        const generatedAtStr = formatMonthDay(data.generated_at || new Date().toISOString());
        const contacts = (data.people || []).map((p: any) => mapPersonToContact(p, generatedAtStr, privacy));
        const raw = data.counts || {};
        const counts: UICounts = {
            ACTIVE: (raw["candidate"] || 0) + (raw["candidate_inbound_only"] || 0),
            PROPOSED: raw["weak_signal"] || 0,
            DORMANT: contacts.filter((contact) => (contact.dormancyDays ?? 0) > DORMANT_DAYS).length,
            EXCLUDED: raw["excluded"] || 0,
        };
        return {contacts, counts};
    } catch (error) {
        console.error("Error loading people data:", error);
        return {contacts: [], counts: EMPTY_COUNTS};
    }
}

export default function PeopleDirectoryPageClient() {
    const [data, setData] = useState<{ contacts: Contact[]; counts: UICounts } | null>(null);

    useEffect(() => {
        let cancelled = false;
        getPeopleData(privacyEnabled()).then((result) => {
            if (!cancelled) setData(result);
        });
        return () => {
            cancelled = true;
        };
    }, []);

    if (data === null) {
        return (
            <main
                className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
        <span className="material-symbols-outlined text-4xl text-primary animate-spin">
          sync
        </span>
                <p className="mt-4 text-ui-medium text-on-surface-variant">Loading Memento Archive...</p>
            </main>
        );
    }

    return <PeopleDirectoryClient initialContacts={data.contacts} initialCounts={data.counts}/>;
}
