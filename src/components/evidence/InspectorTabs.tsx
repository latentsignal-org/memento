"use client";

interface TabOption {
    id: string;
    label: string;
    disabled?: boolean;
}

interface InspectorTabsProps {
    activeTab: string;
    onChange: (tab: string) => void;
    tabs: TabOption[];
}

export default function InspectorTabs({
                                          activeTab,
                                          onChange,
                                          tabs,
                                      }: InspectorTabsProps) {
    return (
        <div className="inline-flex rounded-lg border border-outline-variant/40 bg-surface-container-low p-1">
            {tabs.map((tab) => {
                const active = tab.id === activeTab;
                return (
                    <button
                        key={tab.id}
                        type="button"
                        disabled={tab.disabled}
                        onClick={() => onChange(tab.id)}
                        className={`rounded-md px-3 py-1.5 text-[11px] font-semibold transition ${
                            active
                                ? "bg-background text-primary shadow-sm"
                                : "text-on-surface-variant hover:text-primary"
                        } disabled:cursor-not-allowed disabled:opacity-40`}
                    >
                        {tab.label}
                    </button>
                );
            })}
        </div>
    );
}
