import {Metadata} from "next";
import ProjectsPageClient from "./ProjectsPageClient";

export const metadata: Metadata = {
    title: "Projects | Memento",
    description: "Bounded narratives and life/work projects extracted from long-term history.",
};

export default function ProjectsPage() {
    return <ProjectsPageClient/>;
}
