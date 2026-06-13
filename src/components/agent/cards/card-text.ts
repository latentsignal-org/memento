import {maskEmailAddresses} from "@/lib/contact-display";

export function cleanCardText(text: string) {
    return maskEmailAddresses(text.replace(/\s*\[msg:[^\]]+\]/g, "").replace(/\s+/g, " ").trim());
}
