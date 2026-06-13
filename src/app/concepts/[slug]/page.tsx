import type {Metadata} from "next";
import {staticSlugParams} from "@/lib/static-page";
import ConceptDetailPageClient from "./ConceptDetailPageClient";

export const metadata: Metadata = {
    title: "Concept | Memento",
};

export function generateStaticParams() {
    return staticSlugParams();
}

export default function ConceptDetailPage() {
    return <ConceptDetailPageClient/>;
}
