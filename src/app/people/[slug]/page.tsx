import type {Metadata} from "next";
import {staticSlugParams} from "@/lib/static-page";
import PersonDetailPageClient from "./PersonDetailPageClient";

export const metadata: Metadata = {
    title: "Person | Memento",
};

export function generateStaticParams() {
    return staticSlugParams();
}

export default function PersonPage() {
    return <PersonDetailPageClient/>;
}
