"use client";

import {useEffect, useMemo, useState} from "react";
import Link from "next/link";
import {X} from "lucide-react";
import {useRouter, useSearchParams} from "next/navigation";
import {Contact} from "@/components/contact-detail";
import {contactInitials} from "@/lib/contact-display";
import {mapPersonToContact} from "@/lib/people-mapping";
import EmailReveal from "@/components/email-reveal";
import GroupCard, {type Group as GroupCardGroup} from "@/components/group-card";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

type UICounts = { ACTIVE: number; PROPOSED: number; DORMANT: number; EXCLUDED: number };

interface PeopleDirectoryClientProps {
    initialContacts: Contact[];
    initialCounts: UICounts;
}

type FilterStatus = "ALL" | "ACTIVE" | "PROPOSED" | "DORMANT" | "EXCLUDED";
type SortMode = "LAST_INTERACTION" | "NAME" | "MESSAGE_COUNT" | "DORMANCY";
const DORMANT_DAYS = 90;

const CLASSIFICATION_FOR_STATUS: Partial<Record<FilterStatus, string>> = {
    ACTIVE: "active",
    PROPOSED: "weak_signal",
    EXCLUDED: "excluded",
};

export default function PeopleDirectoryClient({initialContacts, initialCounts}: PeopleDirectoryClientProps) {
    const router = useRouter();
    const searchParams = useSearchParams();
    const searchQuery = searchParams.get("search") || "";
    const [filterStatus, setFilterStatus] = useState<FilterStatus>("ACTIVE");
    const [sortBy, setSortBy] = useState<SortMode>("MESSAGE_COUNT");
    const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set());
    const [dismissing, setDismissing] = useState<string | null>(null);
    const [confirmDismissId, setConfirmDismissId] = useState<string | null>(null);
    const [updatingId, setUpdatingId] = useState<string | null>(null);
    const [contacts, setContacts] = useState<Contact[]>(initialContacts);
    const [isLoadingTab, setIsLoadingTab] = useState(false);
    const [tabCounts, setTabCounts] = useState<UICounts>(initialCounts);
    const [pendingMergeCount, setPendingMergeCount] = useState<number | null>(null);

    useEffect(() => {
        setTabCounts(initialCounts);
    }, [initialCounts]);

    useEffect(() => {
        let cancelled = false;
        fetch("/api/people/merge-suggestions?limit=100", {cache: "no-store"})
            .then((res) => {
                if (!res.ok) throw new Error(`Merge suggestions failed with ${res.status}`);
                return res.json();
            })
            .then((data) => {
                if (!cancelled) setPendingMergeCount((data.suggestions || []).length);
            })
            .catch((err) => {
                if (!cancelled) {
                    console.error("Failed to load merge suggestion count", err);
                    setPendingMergeCount(null);
                }
            });
        return () => {
            cancelled = true;
        };
    }, []);

    useEffect(() => {
        if (filterStatus === "ACTIVE" || filterStatus === "DORMANT") {
            setContacts(initialContacts);
        }
    }, [initialContacts, filterStatus]);

    useEffect(() => {
        const query = searchQuery.trim();
        if (query === "") {
            if (filterStatus === "ACTIVE" || filterStatus === "DORMANT") {
                setContacts(initialContacts);
            }
            return;
        }

        let cancelled = false;
        setIsLoadingTab(true);
        fetch(`/api/people?classification=all&q=${encodeURIComponent(query)}&top=100`)
            .then((res) => {
                if (!res.ok) {
                    throw new Error(`People search failed with ${res.status}`);
                }
                return res.json();
            })
            .then((data) => {
                if (!cancelled) {
                    setContacts((data.people || []).map((p: any) => mapPersonToContact(p)));
                }
            })
            .catch((err) => {
                if (!cancelled) {
                    console.error("Failed to search people", err);
                }
            })
            .finally(() => {
                if (!cancelled) {
                    setIsLoadingTab(false);
                }
            });

        return () => {
            cancelled = true;
        };
    }, [searchQuery, initialContacts, filterStatus]);

    // Auto-clear confirmation state after 15 seconds
    useEffect(() => {
        if (confirmDismissId) {
            const timer = setTimeout(() => setConfirmDismissId(null), 15000);
            return () => clearTimeout(timer);
        }
    }, [confirmDismissId]);

    const handleOverrideClassification = async (
        id: string,
        slug: string,
        target: "human" | "excluded",
        e: React.MouseEvent
    ) => {
        e.preventDefault();
        e.stopPropagation();
        setUpdatingId(id);
        try {
            const res = await fetch(`/api/people/${slug}/override-classification`, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({classification: target}),
            });
            if (!res.ok) {
                const errorText = await res.text();
                alert(`Failed to update classification: ${errorText}`);
                return;
            }
            window.location.reload();
            await handleFilterChange(filterStatus);
        } catch (err) {
            console.error(err);
            alert(`Error updating classification: ${err instanceof Error ? err.message : String(err)}`);
        } finally {
            setUpdatingId(null);
        }
    };

    const [activeTab, setActiveTab] = useState<"directory" | "groups" | "leaderboard">("directory");
    const [groups, setGroups] = useState<GroupCardGroup[]>([]);
    const [leaderboard, setLeaderboard] = useState<any | null>(null);
    const [isLoadingGroups, setIsLoadingGroups] = useState(false);
    const [isLoadingLeaderboard, setIsLoadingLeaderboard] = useState(false);

    const handleTabChange = async (tab: "directory" | "groups" | "leaderboard") => {
        setActiveTab(tab);
        if (tab === "groups" && groups.length === 0) {
            setIsLoadingGroups(true);
            try {
                const res = await fetch("/api/social/groups");
                const data = await res.json();
                setGroups(data.groups || []);
            } catch (err) {
                console.error("Failed to load groups", err);
            } finally {
                setIsLoadingGroups(false);
            }
        } else if (tab === "leaderboard" && !leaderboard) {
            setIsLoadingLeaderboard(true);
            try {
                const res = await fetch("/api/social/leaderboard");
                const data = await res.json();
                setLeaderboard(data || null);
            } catch (err) {
                console.error("Failed to load leaderboard", err);
            } finally {
                setIsLoadingLeaderboard(false);
            }
        }
    };

    const handleGroupChange = (next: GroupCardGroup) => {
        setGroups((prev) => prev.map((g) => (g.group_id === next.group_id ? next : g)));
    };

    const handleGroupRemove = (groupId: number) => {
        setGroups((prev) => prev.filter((g) => g.group_id !== groupId));
    };

    const counts = useMemo(() => ({
        ALL: tabCounts.ACTIVE + tabCounts.PROPOSED + tabCounts.EXCLUDED,
        ACTIVE: tabCounts.ACTIVE,
        PROPOSED: tabCounts.PROPOSED,
        DORMANT: tabCounts.DORMANT,
        EXCLUDED: tabCounts.EXCLUDED,
    }), [tabCounts]);

    const handleFilterChange = async (status: FilterStatus) => {
        setFilterStatus(status);
        if (status === "ACTIVE") {
            setContacts(initialContacts);
            return;
        }
        if (status === "DORMANT") {
            setContacts(initialContacts);
            setSortBy("DORMANCY");
            return;
        }
        const classification = status === "ALL" ? "all" : CLASSIFICATION_FOR_STATUS[status];
        if (!classification) return;
        setIsLoadingTab(true);
        try {
            const res = await fetch(`/api/people?classification=${classification}&top=500`);
            if (!res.ok) {
                throw new Error(`People tab fetch failed with ${res.status}`);
            }
            const data = await res.json();
            setContacts((data.people || []).map((p: any) => mapPersonToContact(p)));
        } catch (err) {
            console.error("Failed to load people tab", err);
            // keep current contacts on fetch error
        } finally {
            setIsLoadingTab(false);
        }
    };

    const metrics = useMemo(() => {
        const totalMessages = initialContacts.reduce((sum, contact) => sum + contact.sourcesCount, 0);
        const domainCounts = initialContacts.reduce<Record<string, number>>((acc, contact) => {
            if (contact.domain) {
                acc[contact.domain] = (acc[contact.domain] || 0) + 1;
            }
            return acc;
        }, {});
        const topDomains = Object.entries(domainCounts)
            .sort((a, b) => b[1] - a[1])
            .slice(0, 6);
        const recentContacts = [...initialContacts]
            .sort((a, b) => new Date(b.lastInteractionTime).getTime() - new Date(a.lastInteractionTime).getTime())
            .slice(0, 5);

        return {
            totalMessages,
            topDomains,
            recentContacts,
            latestGeneratedAt: initialContacts[0]?.lastUpdated || "local export",
        };
    }, [initialContacts]);

    const filteredAndSortedContacts = useMemo(() => {
        let result = contacts.filter((c) => !dismissedIds.has(c.id));

        if (searchQuery.trim() !== "") {
            const query = searchQuery.toLowerCase();
            result = result.filter(
                (contact) =>
                    contact.name.toLowerCase().includes(query) ||
                    contact.organization.toLowerCase().includes(query) ||
                    contact.primaryEmail.toLowerCase().includes(query) ||
                    contact.alias.some((alias) => alias.toLowerCase().includes(query))
            );
        }

        if (filterStatus === "DORMANT") {
            result = result.filter((contact) => (contact.dormancyDays ?? 0) > DORMANT_DAYS);
        }

        result.sort((a, b) => {
            if (sortBy === "NAME") {
                return a.name.localeCompare(b.name);
            }
            if (sortBy === "MESSAGE_COUNT") {
                return b.sourcesCount - a.sourcesCount;
            }
            if (sortBy === "DORMANCY") {
                return (b.dormancyDays ?? 0) - (a.dormancyDays ?? 0);
            }
            return new Date(b.lastInteractionTime).getTime() - new Date(a.lastInteractionTime).getTime();
        });

        return result;
    }, [contacts, dismissedIds, filterStatus, searchQuery, sortBy]);

    const handleDismiss = async (id: string, slug: string, e: React.MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();

        // If not yet confirmed, show confirmation state
        if (confirmDismissId !== id) {
            setConfirmDismissId(id);
            return;
        }

        // Confirmed, proceed with deletion
        setDismissing(id);
        setConfirmDismissId(null);
        try {
            await fetch(`/api/people/${slug}`, {method: "DELETE"});
            setDismissedIds((prev) => new Set(prev).add(id));
        } finally {
            setDismissing(null);
        }
    };

    const handleExport = () => {
        const dataStr = `data:text/json;charset=utf-8,${encodeURIComponent(JSON.stringify(filteredAndSortedContacts, null, 2))}`;
        const downloadAnchor = document.createElement("a");
        downloadAnchor.setAttribute("href", dataStr);
        downloadAnchor.setAttribute("download", `memento_contacts_export_${filterStatus.toLowerCase()}.json`);
        document.body.appendChild(downloadAnchor);
        downloadAnchor.click();
        downloadAnchor.remove();
    };

    const getBadgeClass = (status: string) => {
        switch (status) {
            case "ACTIVE":
                return "bg-primary text-white border-primary";
            case "PROPOSED":
                return "border-dashed border-outline-variant text-on-surface-variant bg-white";
            case "DORMANT":
                return "bg-surface-container-highest text-on-surface-variant border-outline-variant/40";
            case "EXCLUDED":
                return "border-outline-variant text-on-surface-variant bg-surface-container";
            default:
                return "border-outline-variant text-on-surface-variant bg-surface-container";
        }
    };

    const sortLabel = {
        LAST_INTERACTION: "Last Interaction",
        NAME: "Name",
        MESSAGE_COUNT: "Message Count",
        DORMANCY: "Dormancy",
    }[sortBy];

    const nextSort = () => {
        setSortBy((current) => {
            if (current === "LAST_INTERACTION") return "MESSAGE_COUNT";
            if (current === "MESSAGE_COUNT") return "NAME";
            if (current === "NAME") return "DORMANCY";
            return "LAST_INTERACTION";
        });
    };

    const statusBoxes: [string, FilterStatus][] = [
        ["Active", "ACTIVE"],
        ["Proposed", "PROPOSED"],
        ["Dormant", "DORMANT"],
        ["Excluded", "EXCLUDED"],
    ];

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div
                className="w-full max-w-[1440px] mx-auto px-4 sm:px-6 py-8 sm:py-12 grid grid-cols-1 lg:grid-cols-12 gap-8">
                <section className="lg:col-span-9 space-y-8">
                    <header className="space-y-4">
                        <div className="flex flex-wrap items-center gap-4">
                            <h1 className="text-display-lg font-display-lg text-primary tracking-tight max-sm:text-[32px]">
                                People Directory
                            </h1>
                            <div className="flex items-center gap-2 mt-2 sm:mt-0">
                <span
                    className="border border-outline-variant/60 bg-surface-container-low text-on-surface-variant font-mono px-2.5 py-0.5 rounded text-[11px] font-bold">
                  {counts.ACTIVE} PEOPLE
                </span>
                                <span
                                    className="bg-primary-fixed text-on-primary-fixed-variant font-mono px-2.5 py-0.5 rounded text-[11px] font-bold">
                  {metrics.totalMessages.toLocaleString()} MESSAGES
                </span>
                            </div>
                        </div>
                        <p className="text-body-reading font-body-reading text-on-surface-variant max-w-[800px] leading-relaxed">
                            People from your email, ranked by recency, message volume, and shared context.
                        </p>
                    </header>

                    <div
                        className="flex flex-col lg:flex-row lg:items-center justify-between border-b border-outline-variant/60 gap-4 mb-6">
                        <div
                            className="flex gap-1 sm:gap-2 -mb-px overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                            <button
                                onClick={() => handleTabChange("directory")}
                                className={`px-3 sm:px-6 py-3 text-ui-medium font-bold border-b-2 transition-all flex items-center gap-2 cursor-pointer whitespace-nowrap shrink-0 ${
                                    activeTab === "directory"
                                        ? "border-primary text-primary"
                                        : "border-transparent text-on-surface-variant/80 hover:text-on-surface hover:border-outline-variant/40"
                                }`}
                            >
                                <span className="material-symbols-outlined text-base">group</span>
                                Directory
                            </button>
                            <button
                                onClick={() => handleTabChange("groups")}
                                className={`px-3 sm:px-6 py-3 text-ui-medium font-bold border-b-2 transition-all flex items-center gap-2 cursor-pointer whitespace-nowrap shrink-0 ${
                                    activeTab === "groups"
                                        ? "border-primary text-primary"
                                        : "border-transparent text-on-surface-variant/80 hover:text-on-surface hover:border-outline-variant/40"
                                }`}
                            >
                                <span className="material-symbols-outlined text-base">hub</span>
                                Groups
                            </button>
                            <button
                                onClick={() => handleTabChange("leaderboard")}
                                className={`px-3 sm:px-6 py-3 text-ui-medium font-bold border-b-2 transition-all flex items-center gap-2 cursor-pointer whitespace-nowrap shrink-0 ${
                                    activeTab === "leaderboard"
                                        ? "border-primary text-primary"
                                        : "border-transparent text-on-surface-variant/80 hover:text-on-surface hover:border-outline-variant/40"
                                }`}
                            >
                                <span className="material-symbols-outlined text-base">leaderboard</span>
                                Key Contacts
                            </button>
                        </div>

                        <div className="flex flex-wrap items-center gap-3 mb-3 lg:mb-0">
                            <Link
                                href="/people/merge-review"
                                className="inline-flex h-[34px] items-center gap-1.5 rounded-lg border border-outline-variant/70 bg-surface-container-low px-3 text-ui-small font-semibold leading-none text-on-surface-variant shadow-sm transition hover:border-primary/30 hover:bg-primary-fixed hover:text-primary shrink-0"
                            >
                                <span className="material-symbols-outlined text-[16px] leading-none">merge_type</span>
                                Merge People
                                {pendingMergeCount && pendingMergeCount > 0 ? (
                                    <span className="ml-1 rounded-full bg-primary px-1.5 py-0.5 text-[10px] font-bold leading-none text-primary-foreground">
                                        {pendingMergeCount}
                                    </span>
                                ) : null}
                            </Link>

                            {/* Local Search Input */}
                            <div className="relative group flex-1 min-w-[180px] lg:flex-none">
                                <input
                                    type="text"
                                    value={searchQuery}
                                    onChange={(e) => {
                                        const params = new URLSearchParams(window.location.search);
                                        if (e.target.value) {
                                            params.set("search", e.target.value);
                                        } else {
                                            params.delete("search");
                                        }
                                        router.replace(`/people?${params.toString()}`);
                                    }}
                                    className="w-full lg:w-64 rounded-lg border border-outline-variant/70 bg-background px-4 py-2 text-ui-small text-on-surface shadow-sm transition-all placeholder-on-surface-variant/75 focus:border-primary/30 focus:ring-2 focus:ring-primary/15 focus:outline-none pl-4 pr-10"
                                    placeholder="Search contacts..."
                                />
                                <span
                                    className="material-symbols-outlined absolute right-3 top-1/2 -translate-y-1/2 text-on-surface-variant/80 text-[20px] pointer-events-none group-focus-within:text-primary transition-colors">
                  search
                </span>
                            </div>
                        </div>
                    </div>

                    {activeTab === "directory" && (
                        <>
                            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                                {statusBoxes.map(([label, status]) => (
                                    <button
                                        key={status}
                                        onClick={() => handleFilterChange(status)}
                                        className={`text-left bg-surface-container-low border rounded-lg p-4 transition-all cursor-pointer ${
                                            filterStatus === status
                                                ? "border-primary/35 ring-1 ring-primary/20 bg-primary-fixed/20 shadow-sm"
                                                : "border-outline-variant/50 hover:border-outline-variant"
                                        }`}
                                    >
                                        <p className="text-label-caps font-label-caps text-on-surface-variant">{label}</p>
                                        <p className="text-headline-md font-headline-md text-primary mt-2">{counts[status]}</p>
                                    </button>
                                ))}
                            </div>

                            <div
                                className="flex flex-col md:flex-row gap-3 md:items-center border-y border-outline-variant/60 py-4">
                                <DropdownMenu>
                                    <DropdownMenuTrigger
                                        className="flex items-center justify-center md:justify-start gap-2 px-3 py-2 bg-primary text-primary-foreground text-ui-small font-ui-small rounded-lg hover:opacity-95 transition-all cursor-pointer">
                                        <span className="material-symbols-outlined text-sm">filter_list</span>
                                        Filter: {filterStatus === "ALL" ? "All People" : filterStatus.charAt(0) + filterStatus.slice(1).toLowerCase()}
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent align="start"
                                                         className="bg-background border border-outline-variant rounded-lg p-1 shadow-md z-50">
                                        {(["ALL", "ACTIVE", "PROPOSED", "DORMANT", "EXCLUDED"] as FilterStatus[]).map((status, index) => (
                                            <div key={status}>
                                                {index === 1 &&
                                                    <DropdownMenuSeparator className="bg-outline-variant my-1 h-px"/>}
                                                <DropdownMenuItem
                                                    onClick={() => handleFilterChange(status)}
                                                    className="text-ui-medium px-4 py-2 hover:bg-surface-container hover:outline-none rounded cursor-pointer flex justify-between gap-4"
                                                >
                                                    <span>{status === "ALL" ? "All People" : status.charAt(0) + status.slice(1).toLowerCase()}</span>
                                                    <span className="text-ui-small opacity-60">({counts[status]})</span>
                                                </DropdownMenuItem>
                                            </div>
                                        ))}
                                    </DropdownMenuContent>
                                </DropdownMenu>

                                <button
                                    onClick={nextSort}
                                    className="flex items-center justify-center md:justify-start gap-2 px-3 py-2 border border-outline-variant text-on-surface-variant text-ui-small font-ui-small rounded-lg hover:bg-surface-container-low transition-colors cursor-pointer"
                                >
                                    <span className="material-symbols-outlined text-sm">sort</span>
                                    Sort: {sortLabel}
                                </button>

                                <div className="flex-1"/>

                                <button
                                    onClick={handleExport}
                                    className="flex items-center justify-center md:justify-start gap-2 px-3 py-2 border border-outline-variant text-on-surface-variant text-ui-small font-ui-small rounded-lg hover:bg-surface-container-low transition-colors cursor-pointer"
                                >
                                    <span className="material-symbols-outlined text-sm">file_download</span>
                                    Export
                                </button>
                            </div>

                            {isLoadingTab ? (
                                <div className="flex items-center justify-center py-16">
                                    <span
                                        className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                                </div>
                            ) : filteredAndSortedContacts.length > 0 ? (
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                                    {filteredAndSortedContacts.map((contact) => (
                                        <Link
                                            key={contact.id}
                                            href={`/people/${contact.slug}`}
                                            className="group relative flex flex-col justify-between p-6 bg-surface-container-low rounded-2xl border border-outline-variant/40 hover:border-outline-variant hover:bg-white transition-all duration-300 hover:-translate-y-1 hover:shadow-md"
                                        >
                                            <div
                                                className="absolute top-3 right-3 z-10 flex items-center gap-1 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity">
                                                {contact.status === "EXCLUDED" ? (
                                                    <button
                                                        onClick={(e) => handleOverrideClassification(contact.id, contact.slug, "human", e)}
                                                        disabled={updatingId === contact.id}
                                                        title="Include"
                                                        className="p-1 rounded-full text-primary hover:text-primary hover:bg-primary/10 transition-colors disabled:opacity-20 flex items-center justify-center"
                                                    >
                                                        <span
                                                            className="material-symbols-outlined text-[18px]">person_add</span>
                                                    </button>
                                                ) : (
                                                    <button
                                                        onClick={(e) => handleOverrideClassification(contact.id, contact.slug, "excluded", e)}
                                                        disabled={updatingId === contact.id}
                                                        title="Exclude"
                                                        className="p-1 rounded-full text-on-surface-variant/60 hover:text-error hover:bg-error/10 transition-colors disabled:opacity-20 flex items-center justify-center"
                                                    >
                                                        <span
                                                            className="material-symbols-outlined text-[18px]">person_remove</span>
                                                    </button>
                                                )}
                                                <button
                                                    onClick={(e) => handleDismiss(contact.id, contact.slug, e)}
                                                    disabled={dismissing === contact.id}
                                                    aria-label={confirmDismissId === contact.id ? "Confirm dismiss" : "Dismiss contact"}
                                                    title={
                                                        confirmDismissId === contact.id
                                                            ? "Click again to confirm - this will hide the contact permanently"
                                                            : "Dismiss contact: Hides this person permanently from all directories (cannot be undone from the UI)"
                                                    }
                                                    className={`p-1 rounded-full transition-all duration-200 disabled:opacity-20 flex items-center justify-center ${
                                                        confirmDismissId === contact.id
                                                            ? "text-amber-700 bg-amber-500/15 border border-amber-500/40 px-2 gap-1 text-[11px] font-bold"
                                                            : "text-on-surface-variant/40 hover:text-error hover:bg-error/10"
                                                    }`}
                                                >
                                                    <X size={14}/>
                                                    {confirmDismissId === contact.id && <span>Confirm?</span>}
                                                </button>
                                            </div>
                                            <div className="flex items-start gap-4">
                                                <div
                                                    className="w-14 h-14 rounded-xl overflow-hidden bg-surface-container-high border border-outline-variant flex items-center justify-center shrink-0">
                                                    {contact.avatarUrl ? (
                                                        <img
                                                            alt={contact.name}
                                                            className="block w-full h-full object-cover"
                                                            src={contact.avatarUrl}
                                                        />
                                                    ) : (
                                                        <span
                                                            className="text-primary font-bold text-lg select-none">{contactInitials(contact.name)}</span>
                                                    )}
                                                </div>
                                                <div className="min-w-0 flex-1">
                                                    <h2 className="text-headline-md font-headline-md text-primary font-bold group-hover:text-primary-container transition-colors [overflow-wrap:anywhere]">
                                                        {contact.name}
                                                    </h2>
                                                    <p className="text-ui-small text-on-surface-variant font-mono mt-0.5 [overflow-wrap:anywhere]">
                                                        <EmailReveal email={contact.primaryEmail}/>
                                                    </p>
                                                    {contact.lastInteraction && (
                                                        <p className="text-[11px] text-on-surface-variant mt-2">
                                                            <span
                                                                className="text-on-surface-variant/60">Last contact</span> {contact.lastInteraction}
                                                        </p>
                                                    )}
                                                </div>
                                            </div>

                                            <div
                                                className="border-t border-outline-variant/40 mt-5 pt-4 flex items-center justify-between gap-3">
                                                <div className="flex flex-wrap gap-2 min-w-0">
                          <span
                              className={`border px-2 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider ${getBadgeClass(contact.status)}`}>
                            {contact.status}
                          </span>
                                                    {(contact.structuralRole === "hub" || contact.structuralRole === "bridge") && (
                                                        <span
                                                            className={`px-2 py-0.5 rounded text-[10px] font-mono uppercase font-bold tracking-wider ${contact.structuralRole === "hub" ? "bg-primary text-white" : "bg-primary-fixed text-on-primary-fixed-variant"}`}>
                              {contact.structuralRole}
                            </span>
                                                    )}
                                                    <span
                                                        className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold">
                            {contact.sourcesCount} messages
                          </span>
                                                    {filterStatus === "DORMANT" && contact.dormancyDays != null && (
                                                        <span
                                                            className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold">
                              {contact.dormancyDays}d inactive
                            </span>
                                                    )}
                                                    {contact.domain && contact.organization !== "Personal" && (
                                                        <span
                                                            className="bg-surface-container-high text-on-surface-variant px-2.5 py-0.5 rounded text-[10px] font-mono font-bold truncate max-w-[150px]">
                              @{contact.domain}
                            </span>
                                                    )}
                                                </div>
                                                <span
                                                    className="material-symbols-outlined text-primary text-lg opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                          arrow_forward
                        </span>
                                            </div>
                                        </Link>
                                    ))}
                                </div>
                            ) : (
                                <div
                                    className="text-center py-16 border border-dashed border-outline-variant rounded-2xl bg-surface-container-low">
                  <span className="material-symbols-outlined text-5xl text-on-surface-variant/40 mb-3">
                    group_off
                  </span>
                                    <h3 className="text-headline-md font-headline-md text-on-surface-variant font-bold mb-2">
                                        No People Found
                                    </h3>
                                    <p className="text-ui-medium text-on-surface-variant max-w-md mx-auto">
                                        No relationship records match the current filter.
                                    </p>
                                </div>
                            )}
                        </>
                    )}

                    {activeTab === "groups" && (
                        <>
                            {isLoadingGroups ? (
                                <div className="flex items-center justify-center py-16">
                                    <span
                                        className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                                </div>
                            ) : groups.length > 0 ? (
                                <>
                                    {(() => {
                                        const saved = groups.filter((g) => !!g.saved_at);
                                        const candidates = groups.filter((g) => !g.saved_at);
                                        return (
                                            <div className="space-y-8">
                                                {saved.length > 0 && (
                                                    <section>
                                                        <div className="flex items-baseline justify-between mb-3">
                                                            <h2 className="text-ui-small font-bold uppercase tracking-wider text-primary">
                                                                Saved groups
                                                            </h2>
                                                            <span className="text-[11px] text-on-surface-variant/70">
                                {saved.length} saved
                              </span>
                                                        </div>
                                                        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                                                            {saved.map((group) => (
                                                                <GroupCard
                                                                    key={group.group_id}
                                                                    group={group}
                                                                    onChange={handleGroupChange}
                                                                    onRemove={handleGroupRemove}
                                                                />
                                                            ))}
                                                        </div>
                                                    </section>
                                                )}
                                                {candidates.length > 0 && (
                                                    <section>
                                                        <div className="flex items-baseline justify-between mb-3">
                                                            <h2 className="text-ui-small font-bold uppercase tracking-wider text-on-surface-variant">
                                                                Suggested groups
                                                            </h2>
                                                            <span className="text-[11px] text-on-surface-variant/70">
                                Detected from shared threads — save the ones worth keeping
                              </span>
                                                        </div>
                                                        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                                                            {candidates.map((group) => (
                                                                <GroupCard
                                                                    key={group.group_id}
                                                                    group={group}
                                                                    onChange={handleGroupChange}
                                                                    onRemove={handleGroupRemove}
                                                                />
                                                            ))}
                                                        </div>
                                                    </section>
                                                )}
                                            </div>
                                        );
                                    })()}
                                </>
                            ) : (
                                <div
                                    className="text-center py-16 border border-dashed border-outline-variant rounded-2xl bg-surface-container-low">
                                    <span
                                        className="material-symbols-outlined text-5xl text-on-surface-variant/40 mb-3">hub</span>
                                    <h3 className="text-headline-md font-headline-md text-on-surface-variant font-bold mb-2">No
                                        Groups Found</h3>
                                    <p className="text-ui-medium text-on-surface-variant">Run <code>memento
                                        refresh</code> to detect groups from your archive.</p>
                                </div>
                            )}
                        </>
                    )}

                    {activeTab === "leaderboard" && (
                        <>
                            {isLoadingLeaderboard ? (
                                <div className="flex items-center justify-center py-16">
                                    <span
                                        className="material-symbols-outlined text-4xl text-primary animate-spin">sync</span>
                                </div>
                            ) : leaderboard ? (
                                <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
                                    {/* 1. Top Collaborators */}
                                    <div
                                        className="bg-surface-container-low/60 border border-outline-variant/40 rounded-2xl p-5 space-y-4">
                                        <div className="flex items-center gap-2.5">
                                            <div
                                                className="w-8 h-8 rounded-lg bg-primary/10 flex items-center justify-center text-primary">
                                                <span className="material-symbols-outlined text-base">stars</span>
                                            </div>
                                            <div>
                                                <h3 className="text-ui-medium font-bold text-on-surface">Top
                                                    Collaborators</h3>
                                                <p className="text-[10px] text-on-surface-variant">Highest weighted
                                                    degree in the graph</p>
                                            </div>
                                        </div>
                                        <div className="space-y-3">
                                            {leaderboard.collaborators?.map((p: any, idx: number) => {
                                                const contact = mapPersonToContact(p);
                                                return (
                                                    <Link
                                                        key={p.person_id}
                                                        href={`/people/${contact.slug}`}
                                                        className="block p-3 rounded-xl border border-outline-variant/30 bg-white hover:border-outline hover:-translate-y-0.5 transition-all duration-200"
                                                    >
                                                        <div className="flex items-center gap-3">
                                                            <span
                                                                className="text-xs font-mono font-bold text-primary w-4">#{idx + 1}</span>
                                                            <div className="min-w-0 flex-1">
                                                                <p className="text-xs font-bold text-on-surface truncate">{contact.name}</p>
                                                                <p className="text-[10px] text-on-surface-variant truncate">
                                                                    <EmailReveal email={contact.primaryEmail}/></p>
                                                            </div>
                                                            <div className="text-right shrink-0">
                                <span
                                    className="inline-block px-1.5 py-0.5 rounded text-[9px] font-mono font-bold bg-primary/10 text-primary">
                                  W: {p.weighted_degree?.toFixed(0)}
                                </span>
                                                            </div>
                                                        </div>
                                                    </Link>
                                                );
                                            })}
                                            {(!leaderboard.collaborators || leaderboard.collaborators.length === 0) && (
                                                <p className="text-xs text-on-surface-variant/60 italic text-center py-4">No
                                                    top collaborators</p>
                                            )}
                                        </div>
                                    </div>

                                    {/* 2. Key Dormant Contacts */}
                                    <div
                                        className="bg-surface-container-low/60 border border-outline-variant/40 rounded-2xl p-5 space-y-4">
                                        <div className="flex items-center gap-2.5">
                                            <div
                                                className="w-8 h-8 rounded-lg bg-primary/10 flex items-center justify-center text-primary">
                                                <span className="material-symbols-outlined text-base">snooze</span>
                                            </div>
                                            <div>
                                                <h3 className="text-ui-medium font-bold text-on-surface">Dormant Key
                                                    Contacts</h3>
                                                <p className="text-[10px] text-on-surface-variant">High history, no
                                                    contact in &gt;90 days</p>
                                            </div>
                                        </div>
                                        <div className="space-y-3">
                                            {leaderboard.dormant?.map((p: any, idx: number) => {
                                                const contact = mapPersonToContact(p);
                                                return (
                                                    <Link
                                                        key={p.person_id}
                                                        href={`/people/${contact.slug}`}
                                                        className="block p-3 rounded-xl border border-outline-variant/30 bg-white hover:border-outline hover:-translate-y-0.5 transition-all duration-200"
                                                    >
                                                        <div className="flex items-center gap-3">
                                                            <span
                                                                className="text-xs font-mono font-bold text-on-surface-variant/60 w-4">#{idx + 1}</span>
                                                            <div className="min-w-0 flex-1">
                                                                <p className="text-xs font-bold text-on-surface truncate">{contact.name}</p>
                                                                <p className="text-[10px] text-on-surface-variant truncate">
                                                                    <EmailReveal email={contact.primaryEmail}/></p>
                                                            </div>
                                                            <div className="text-right shrink-0">
                                <span
                                    className="inline-block px-1.5 py-0.5 rounded text-[9px] font-mono font-bold bg-amber-500/10 text-amber-600">
                                  {p.dormancy_days}d
                                </span>
                                                            </div>
                                                        </div>
                                                    </Link>
                                                );
                                            })}
                                            {(!leaderboard.dormant || leaderboard.dormant.length === 0) && (
                                                <p className="text-xs text-on-surface-variant/60 italic text-center py-4">No
                                                    dormant key contacts</p>
                                            )}
                                        </div>
                                    </div>

                                    {/* 3. Structural Bridges */}
                                    <div
                                        className="bg-surface-container-low/60 border border-outline-variant/40 rounded-2xl p-5 space-y-4">
                                        <div className="flex items-center gap-2.5">
                                            <div
                                                className="w-8 h-8 rounded-lg bg-primary/10 flex items-center justify-center text-primary">
                                                <span className="material-symbols-outlined text-base">mediation</span>
                                            </div>
                                            <div>
                                                <h3 className="text-ui-medium font-bold text-on-surface">Structural
                                                    Bridges</h3>
                                                <p className="text-[10px] text-on-surface-variant">Connecting otherwise
                                                    disjoint sub-groups</p>
                                            </div>
                                        </div>
                                        <div className="space-y-3">
                                            {leaderboard.bridges?.map((p: any, idx: number) => {
                                                const contact = mapPersonToContact(p);
                                                return (
                                                    <Link
                                                        key={p.person_id}
                                                        href={`/people/${contact.slug}`}
                                                        className="block p-3 rounded-xl border border-outline-variant/30 bg-white hover:border-outline hover:-translate-y-0.5 transition-all duration-200"
                                                    >
                                                        <div className="flex items-center gap-3">
                                                            <span
                                                                className="text-xs font-mono font-bold text-primary w-4">#{idx + 1}</span>
                                                            <div className="min-w-0 flex-1">
                                                                <p className="text-xs font-bold text-on-surface truncate">{contact.name}</p>
                                                                <p className="text-[10px] text-on-surface-variant truncate">
                                                                    <EmailReveal email={contact.primaryEmail}/></p>
                                                            </div>
                                                            <div className="text-right shrink-0">
                                <span
                                    className="inline-block px-1.5 py-0.5 rounded text-[9px] font-mono font-bold bg-indigo-500/10 text-indigo-600">
                                  Bridge
                                </span>
                                                            </div>
                                                        </div>
                                                    </Link>
                                                );
                                            })}
                                            {(!leaderboard.bridges || leaderboard.bridges.length === 0) && (
                                                <p className="text-xs text-on-surface-variant/60 italic text-center py-4">No
                                                    structural bridges found</p>
                                            )}
                                        </div>
                                    </div>
                                </div>
                            ) : (
                                <div
                                    className="text-center py-16 border border-dashed border-outline-variant rounded-2xl bg-surface-container-low">
                                    <span
                                        className="material-symbols-outlined text-5xl text-on-surface-variant/40 mb-3">leaderboard</span>
                                    <h3 className="text-headline-md font-headline-md text-on-surface-variant font-bold mb-2">No
                                        Leaderboard Found</h3>
                                    <p className="text-ui-medium text-on-surface-variant">Run Memento refresh or build
                                        graph to populate metrics.</p>
                                </div>
                            )}
                        </>
                    )}
                </section>

                <aside className="lg:col-span-3 space-y-6">
                    <div className="bg-primary text-primary-foreground rounded-2xl p-6">
                        <h2 className="text-ui-medium font-bold mb-4">Relationship Snapshot</h2>
                        <div className="space-y-4">
                            <div>
                                <p className="text-[11px] uppercase tracking-wider opacity-70">Resolved People</p>
                                <p className="text-headline-md font-headline-md">{counts.ACTIVE}</p>
                            </div>
                            <div>
                                <p className="text-[11px] uppercase tracking-wider opacity-70">Mapped Messages</p>
                                <p className="text-headline-md font-headline-md">{metrics.totalMessages.toLocaleString()}</p>
                            </div>
                            <div>
                                <p className="text-[11px] uppercase tracking-wider opacity-70">Generated</p>
                                <p className="text-ui-small font-bold">{metrics.latestGeneratedAt}</p>
                            </div>
                        </div>
                    </div>

                    <div className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6">
                        <h2 className="text-ui-medium font-bold text-primary mb-4">Recent Signals</h2>
                        <div className="space-y-3">
                            {metrics.recentContacts.map((contact) => (
                                <Link
                                    href={`/people/${contact.slug}`}
                                    key={`recent-${contact.id}`}
                                    className="block border border-outline-variant/40 rounded-lg p-3 bg-white hover:border-outline transition-colors"
                                >
                                    <div className="flex items-center justify-between gap-3">
                                        <span
                                            className="text-ui-small font-bold text-on-surface truncate">{contact.name}</span>
                                        <span
                                            className="font-mono text-[10px] text-primary font-bold">{contact.sourcesCount}</span>
                                    </div>
                                    <p className="text-[11px] text-on-surface-variant line-clamp-1 mt-1">
                                        {contact.lastInteraction}
                                    </p>
                                </Link>
                            ))}
                        </div>
                    </div>

                    <div className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6">
                        <h2 className="text-ui-medium font-bold text-primary mb-4">Top Domains</h2>
                        <div className="space-y-3">
                            {metrics.topDomains.map(([domain, count]) => (
                                <div key={domain} className="flex items-center justify-between text-ui-small">
                                    <span className="text-on-surface-variant font-mono truncate">@{domain}</span>
                                    <span className="font-mono text-primary font-bold">{count}</span>
                                </div>
                            ))}
                        </div>
                    </div>
                </aside>
            </div>

            <footer className="w-full border-t border-outline-variant/40 bg-surface-container-low/40 py-12 mt-12">
                <div className="max-w-[1440px] mx-auto px-4 sm:px-6 grid grid-cols-1 md:grid-cols-2 gap-12">
                    <div>
            <span className="text-label-caps text-primary mb-4 block font-bold">
              DIMENSIONAL MEMORY
            </span>
                        <div
                            className="text-ui-medium text-on-surface italic font-display-lg leading-relaxed max-w-[500px]">
                            &ldquo;People are the stable anchors in the archive. Every project, thread, and follow-up
                            becomes more legible once correspondence is resolved to real relationships.&rdquo;
                        </div>
                    </div>
                    <div className="space-y-4 max-w-[500px]">
                        <div className="flex justify-between items-center text-ui-small font-bold">
                            <span className="text-on-surface-variant">Relationship Coverage</span>
                            <span className="text-primary">Live Export</span>
                        </div>
                        <div className="w-full bg-surface-container h-1.5 rounded-full overflow-hidden">
                            <div className="bg-primary h-full w-[72%]"/>
                        </div>
                        <p className="text-[11px] text-on-surface-variant leading-relaxed">
                            People records are generated from resolved participant clusters and are linked back to
                            message timelines for citation-ready detail pages.
                        </p>
                    </div>
                </div>
            </footer>
        </main>
    );
}
