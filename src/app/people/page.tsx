import {Suspense} from "react";
import type {Metadata} from "next";
import PeopleDirectoryPageClient from "./PeopleDirectoryPageClient";

export const metadata: Metadata = {
    title: "People | Memento",
    description: "Relationship wikis for the meaningful human contacts in your archive.",
};

export default function PeopleDirectoryPage() {
    return (
        <Suspense fallback={null}>
            <PeopleDirectoryPageClient/>
        </Suspense>
    );
}
