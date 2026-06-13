import type {Metadata} from "next";
import ConceptsPageClient from "./ConceptsPageClient";

export const metadata: Metadata = {
    title: "Concepts | Memento",
    description: "Evergreen knowledge pages — opt-in topics curated from your archive.",
};

export default function ConceptsPage() {
    return <ConceptsPageClient/>;
}
