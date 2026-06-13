import type {Metadata} from "next";
import {staticSlugParams} from "@/lib/static-page";
import NewsletterDetailPageClient from "./NewsletterDetailPageClient";

export const metadata: Metadata = {
    title: "Newsletter | Memento",
};

export function generateStaticParams() {
    return staticSlugParams();
}

export default function NewsletterDetailPage() {
    return <NewsletterDetailPageClient/>;
}
