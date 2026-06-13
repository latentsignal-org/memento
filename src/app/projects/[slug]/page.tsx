import type {Metadata} from "next";
import {staticSlugParams} from "@/lib/static-page";
import ProjectDetailPageClient from "./ProjectDetailPageClient";

export const metadata: Metadata = {
    title: "Project | Memento",
};

export function generateStaticParams() {
    return staticSlugParams();
}

export default function ProjectPage() {
    return <ProjectDetailPageClient/>;
}
