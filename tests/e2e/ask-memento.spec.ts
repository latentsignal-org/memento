import {expect, test} from "@playwright/test";

test("ask memento accepts a question with the fake provider", async ({page}) => {
    await page.goto("/ask");

    await expect(page.getByRole("heading", {name: "Ask Memento"})).toBeVisible();
    await expect(page.getByText("Archive search session")).toBeVisible();

    await page
        .getByPlaceholder("Ask a follow-up or explore a person/project/concept...")
        .fill("What does the fixture cover?");
    await page.getByRole("button", {name: "Send", exact: true}).click();

    await expect(page.getByText("What does the fixture cover?")).toBeVisible();
    await expect(page.getByText("Fake agent run complete.").first()).toBeVisible();
});
