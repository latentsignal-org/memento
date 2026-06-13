import type {Metadata} from "next";
import {SetupWizard} from "@/components/setup/SetupWizard";

export const metadata: Metadata = {
    title: "Onboarding | Memento",
    description: "Connect a local archive and build Memento's memory surfaces.",
};

export default function OnboardPage() {
    return <SetupWizard/>;
}
