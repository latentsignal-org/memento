import {Metadata} from "next";
import NewslettersPageClient from "./NewslettersPageClient";

export const metadata: Metadata = {
    title: "Newsletters | Memento",
    description: "Newsletter sources and coverage summaries extracted from long-term email history.",
};

export default function NewslettersPage() {
    return <NewslettersPageClient/>;
}
