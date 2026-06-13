import {Metadata} from "next";
import {Suspense} from "react";
import DashboardPageClient from "./DashboardPageClient";

export const metadata: Metadata = {
    title: "Home | Memento",
    description: "Executive brief synthesized from local People, Projects, and Newsletter dimensions.",
};

export default function DashboardPage() {
    return (
        <Suspense fallback={null}>
            <DashboardPageClient/>
        </Suspense>
    );
}
