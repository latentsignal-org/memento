// Centralized step configurations for agent progress bars.
// Each dimension defines which tool_call_start events map to which progress step.
// Update this file when the backend tool list changes — no need to touch *Button.tsx.
import type {AgentEvent} from "./agent-events";

export interface StepDef {
    key: string;
    label: string;
}

export interface StepConfig {
    steps: readonly StepDef[];
    /** Map a tool name to a step key. For section-based tools, use sectionToStep instead. */
    toolToStep: Record<string, string>;
    /** For write_*_section tools, map { toolName: { sectionValue: stepKey } }. */
    sectionToStep?: Record<string, Record<string, string>>;
    /** Step key to fill on the very first tool_call_start of any kind (e.g. "profile already loaded"). */
    fillOnFirstTool?: string;
}

function computeCompletedSteps(config: StepConfig, events: AgentEvent[]): Set<string> {
    const steps = new Set<string>();
    let firstToolSeen = false;
    for (const event of events) {
        if (event.type !== "tool_call_start") continue;
        if (!firstToolSeen && config.fillOnFirstTool) {
            steps.add(config.fillOnFirstTool);
            firstToolSeen = true;
        }
        const mapped = config.toolToStep[event.name];
        if (mapped) steps.add(mapped);
        if (config.sectionToStep?.[event.name]) {
            const section = (event.args as { section?: string })?.section;
            if (section && config.sectionToStep[event.name][section]) {
                steps.add(config.sectionToStep[event.name][section]);
            }
        }
    }
    return steps;
}

export const PERSON_STEPS_CONFIG: StepConfig = {
    steps: [
        {key: "profile", label: "Profile"},
        {key: "messages", label: "Messages"},
        {key: "social", label: "Social Graph"},
        {key: "details", label: "Details"},
        {key: "facets", label: "Facets"},
        {key: "relationship_arc", label: "History"},
        {key: "current_status", label: "Status"},
    ] as const,
    toolToStep: {
        list_person_messages: "messages",
        get_message: "messages",
        get_message_batch: "messages",
        fts_search_scoped: "messages",
        fts_search: "messages",
        vector_search: "messages",
        get_thread: "messages",
        write_person_attribute: "details",
        write_facet: "facets",
        get_person_network: "social",
        get_group: "social",
        get_cluster: "social",
    },
    sectionToStep: {
        write_person_section: {
            summary: "profile",
            relationship_arc: "relationship_arc",
            current_status: "current_status",
        },
    },
    fillOnFirstTool: "profile",
};

export const PROJECT_STEPS_CONFIG: StepConfig = {
    steps: [
        {key: "bundle", label: "Bundle"},
        {key: "context", label: "Context"},
        {key: "summary", label: "Summary"},
        {key: "phases", label: "Phases"},
        {key: "friction_points", label: "Friction"},
        {key: "current_understanding", label: "Status"},
    ] as const,
    toolToStep: {
        get_project_bundle: "bundle",
        detect_gaps: "context",
        fts_search: "context",
        vector_search: "context",
        get_message: "context",
        get_message_batch: "context",
        summarize_thread: "context",
    },
    sectionToStep: {
        write_section: {
            summary: "summary",
            phases: "phases",
            friction_points: "friction_points",
            current_understanding: "current_understanding",
        },
    },
};

export const CONCEPT_STEPS_CONFIG: StepConfig = {
    steps: [
        {key: "bundle", label: "Bundle"},
        {key: "cluster", label: "Cluster"},
        {key: "scope_summary", label: "Scope"},
        {key: "distilled_insights", label: "Insights"},
        {key: "evolving_understanding", label: "Evolution"},
    ] as const,
    toolToStep: {
        get_concept_bundle: "bundle",
        cluster_messages_by_subject: "cluster",
        get_cluster: "cluster",
        fts_search: "cluster",
        vector_search: "cluster",
        get_message: "cluster",
        get_message_batch: "cluster",
        summarize_thread: "cluster",
    },
    sectionToStep: {
        write_concept_section: {
            scope_summary: "scope_summary",
            distilled_insights: "distilled_insights",
            evolving_understanding: "evolving_understanding",
        },
    },
};

export {computeCompletedSteps};
