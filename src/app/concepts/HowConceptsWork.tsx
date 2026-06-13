import Link from "next/link";

export default function HowConceptsWork() {
    const sections = [
        {
            id: "declare",
            title: "Declare",
            icon: "lightbulb",
            content: "Start by naming an evergreen topic you want Memento to track. Concepts are opt-in, not auto-created.",
        },
        {
            id: "backfill",
            title: "Backfill",
            icon: "manage_search",
            content: "Memento searches across newsletters, projects, and people to gather source messages that define the topic.",
        },
        {
            id: "generate",
            title: "Generate",
            icon: "auto_awesome",
            content: "Generate a living concept page with a scoped explanation and source-backed synthesis.",
        },
        {
            id: "maintain",
            title: "Maintain",
            icon: "edit",
            content: "Your edits remain part of the concept while future refreshes add relevant new archive context.",
        },
    ];

    return (
        <div className="space-y-4 rounded-2xl border border-outline-variant/40 bg-surface-container-low p-6">
            <div className="mb-2 flex items-center gap-2 text-primary">
                <span className="material-symbols-outlined text-xl">info</span>
                <h3 className="text-ui-medium font-bold font-ui-medium">How Concepts Work</h3>
            </div>

            <p className="-mt-2 text-[11px] leading-relaxed text-on-surface-variant">
                Concepts are user-declared knowledge pages maintained from your archive.
            </p>

            <div className="space-y-2">
                {sections.map((section) => (
                    <details key={section.id} className="group">
                        <summary
                            className="flex cursor-pointer list-none items-center gap-2 rounded-lg px-3 py-2 transition-colors hover:bg-surface-container">
              <span
                  className="material-symbols-outlined flex-shrink-0 text-[18px] text-primary transition-transform group-hover:scale-110">
                {section.icon}
              </span>
                            <span className="text-ui-small flex-1 font-semibold text-on-surface">
                {section.title}
              </span>
                            <span
                                className="material-symbols-outlined text-[18px] text-on-surface-variant transition-transform group-open:rotate-180">
                expand_more
              </span>
                        </summary>
                        <div
                            className="mx-2 rounded-lg border-l-2 border-primary/30 bg-surface-container-high px-3 py-2 text-[11px] leading-relaxed text-on-surface-variant">
                            {section.content}
                        </div>
                    </details>
                ))}
            </div>

            <Link
                href="/concepts/new"
                className="inline-flex w-full items-center justify-center rounded bg-primary px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:opacity-90"
            >
                + New concept
            </Link>
        </div>
    );
}
