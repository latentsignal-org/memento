import {expect, test} from "@playwright/test";

test("newsletters index opens seeded newsletter detail", async ({page}) => {
    await page.goto("/newsletters");

    await expect(page.getByRole("heading", {name: "Newsletter Sources"})).toBeVisible();
    const newsletterLink = page.getByRole("link", {name: /Fixture Digest/i}).first();
    await expect(newsletterLink).toBeVisible();
    await newsletterLink.click();

    await expect(page).toHaveURL(/\/newsletters\/fixture-digest$/);
    await expect(page.getByRole("heading", {name: "Fixture Digest"})).toBeVisible();
    await expect(page.getByText("2 messages").first()).toBeVisible();
    await expect(page.getByText("Fixture Digest tracks local-first memory patterns")).toBeVisible();
    await expect(page.getByRole("heading", {name: "Recurring Themes"})).toBeVisible();
    await expect(page.getByText("Testing local-first product workflows")).toBeVisible();
    await expect(page.getByRole("heading", {name: "Message Timeline"})).toBeVisible();
});
