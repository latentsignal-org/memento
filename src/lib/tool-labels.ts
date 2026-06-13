const TOOL_LABELS: Record<string, string> = {
    fts_search: "Run full text search",
    fts_search_scoped: "Run scoped full text search",
    vector_search: "Run semantic search",
    get_message: "Read message",
    get_message_batch: "Read messages in batch",
    get_thread: "Read thread",
    summarize_thread: "Summarize thread",
    cluster_messages_by_subject: "Cluster themes",
    find_people: "Find people matches",
    search_persons: "Search people",
    get_person_summary: "Load person summary",
    get_person_network: "Load person network",
    list_person_messages: "Review person messages",
    find_missing_collaborators: "Find missing collaborators",
    find_bridges_between: "Find bridges between people",
    get_bundle_index: "Load bundle index",
    get_project_bundle: "Load project bundle",
    get_concept_bundle: "Load concept bundle",
    get_project_summary: "Load project summary",
    get_concept_summary: "Load concept summary",
    get_cluster: "Load communication cluster",
    get_group: "Load communication group",
    get_notes: "Load notes",
    search_projects: "Search projects",
    search_concepts: "Search concepts",
    write_facet: "Write facet",
    write_person_attribute: "Write structured attribute",
    write_person_section: "Write person narrative",
    detect_gaps: "Check continuity gaps",
    detect_gaps_with_results: "Detect gaps with results",
    propose_bundle: "Stage bundle",
    propose_backfill: "Propose backfill",
    create_project_draft: "Create project draft",
    create_concept_draft: "Create concept draft",
    add_project_messages: "Add messages to project",
    add_concept_messages: "Add messages to concept",
    context_status: "Check context budget",
    "refresh-projects-rollup": "Refresh project summaries",
    "refresh-concepts-rollup": "Refresh concept summaries",
    "refresh-people-rollup": "Refresh people summaries",
};

function formatSectionName(value: string): string {
    return value
        .split("_")
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(" ");
}

function sectionFromArgs(args: unknown): string | null {
    if (!args || typeof args !== "object") return null;
    const section = (args as { section?: unknown }).section;
    return typeof section === "string" && section.length > 0 ? section : null;
}

export function getToolLabel(name: string, args?: unknown): string {
    if (name === "write_section") {
        const section = sectionFromArgs(args);
        return section ? `Write narrative: ${formatSectionName(section)}` : "Write narrative";
    }

    if (name === "write_concept_section") {
        const section = sectionFromArgs(args);
        return section ? `Write concept narrative: ${formatSectionName(section)}` : "Write concept narrative";
    }

    if (name === "write_person_section") {
        const section = sectionFromArgs(args);
        return section ? `Write person narrative: ${formatSectionName(section)}` : "Write person narrative";
    }

    return TOOL_LABELS[name] ?? name;
}
