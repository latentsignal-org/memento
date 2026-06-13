import type {Metadata} from "next";
import SessionsPageClient from "./SessionsPageClient";

export const metadata: Metadata = {
    title: "Sessions | Memento",
    description: "Saved Ask Memento investigations with source-backed answers and context references.",
};

export default function SessionsPage() {
    return <SessionsPageClient/>;
}
