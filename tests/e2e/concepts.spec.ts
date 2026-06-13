import {expect, test} from "@playwright/test";

test("concepts index opens seeded concept detail", async ({page}) => {
    await page.goto("/concepts");

    await expect(page.getByRole("heading", {name: "Concepts", exact: true})).toBeVisible();
    await expect(page.getByText("YOUR DECLARED CONCEPTS")).toBeVisible();
    const conceptLink = page.getByRole("link", {name: /Local-first Memory/i}).first();
    await expect(conceptLink).toBeVisible();
    await conceptLink.click();

    await expect(page).toHaveURL(/\/concepts\/local-first-memory$/);
    await expect(page.getByRole("heading", {name: "Local-first Memory"})).toBeVisible();
    await expect(page.getByText("How Memento turns a local email archive")).toBeVisible();
    await expect(page.getByText("Local-first Memory covers the way Memento keeps source-attributed knowledge")).toBeVisible();
    await expect(page.getByRole("heading", {name: "Disposable fixtures keep E2E runs isolated"})).toBeVisible();
    await expect(page.getByText("MOST RECENT MENTIONS")).toBeVisible();
    await expect(page.getByText("Casey Rivera").first()).toBeVisible();
    await expect(page.getByText("Fixture Digest").first()).toBeVisible();
});
