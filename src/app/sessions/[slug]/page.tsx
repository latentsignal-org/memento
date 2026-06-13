import type {Metadata} from "next";
import {staticSlugParams} from "@/lib/static-page";
import SessionDetailPageClient from "./SessionDetailPageClient";

export const metadata: Metadata = {
    title: "Session | Memento",
};

export function generateStaticParams() {
    return staticSlugParams();
}

export default function SessionDetailPage() {
    return <SessionDetailPageClient/>;
}
