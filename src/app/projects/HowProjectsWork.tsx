export default function HowProjectsWork() {
    const sections = [
        {
            id: "create",
            title: "Create",
            icon: "edit_note",
            content: "Start by clicking + New project and describing what you're looking for—a home renovation, work initiative, or any subject.",
        },
        {
            id: "search",
            title: "Search & Curate",
            icon: "manage_search",
            content: "Memento finds relevant messages and people. You refine the selection, add missing pieces, and remove what doesn't belong.",
        },
        {
            id: "generate",
            title: "Generate",
            icon: "auto_awesome",
            content: "Click Generate with AI on the project page. The system identifies phases and friction points, then writes a narrative with citations.",
        },
        {
            id: "edit",
            title: "Edit & Preserve",
            icon: "edit",
            content: "All generated content is editable. Your changes are preserved if you regenerate the narrative later.",
        },
    ];

    return (
        <div className="bg-surface-container-low border border-outline-variant/40 rounded-2xl p-6 space-y-4">
            <div className="flex items-center gap-2 text-primary mb-2">
                <span className="material-symbols-outlined text-xl">info</span>
                <h3 className="text-ui-medium font-bold font-ui-medium">How Projects Work</h3>
            </div>

            <p className="text-[11px] text-on-surface-variant leading-relaxed -mt-2">
                Projects are stories built from your email history.
            </p>

            <div className="space-y-2">
                {sections.map((section) => (
                    <details key={section.id} className="group">
                        <summary
                            className="flex items-center gap-2 px-3 py-2 rounded-lg hover:bg-surface-container transition-colors cursor-pointer list-none">
              <span
                  className="material-symbols-outlined text-[18px] text-primary group-hover:scale-110 transition-transform flex-shrink-0">
                {section.icon}
              </span>
                            <span className="text-ui-small font-semibold text-on-surface flex-1">
                {section.title}
              </span>
                            <span
                                className="material-symbols-outlined text-[18px] text-on-surface-variant transition-transform group-open:rotate-180">
                expand_more
              </span>
                        </summary>
                        <div
                            className="px-3 py-2 text-[11px] text-on-surface-variant leading-relaxed bg-surface-container-high rounded-lg mx-2 border-l-2 border-primary/30">
                            {section.content}
                        </div>
                    </details>
                ))}
            </div>
        </div>
    );
}
