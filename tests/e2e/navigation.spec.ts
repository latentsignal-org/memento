import {expect, test} from "@playwright/test";

test("home links navigate to seeded dimension pages", async ({page}) => {
    await page.goto("/home");

    await page.getByRole("link", {name: /Fixture Launch/i}).first().click();
    await expect(page).toHaveURL(/\/projects\/fixture-launch$/);
    await expect(page.getByRole("heading", {name: "Fixture Launch"})).toBeVisible();

    await page.goto("/home");
    await page.getByRole("link", {name: /Local-first Memory/i}).first().click();
    await expect(page).toHaveURL(/\/concepts\/local-first-memory$/);
    await expect(page.getByRole("heading", {name: "Local-first Memory"})).toBeVisible();

    await page.goto("/home");
    await page.getByRole("link", {name: /Fixture Digest/i}).first().click();
    await expect(page).toHaveURL(/\/newsletters\/fixture-digest$/);
    await expect(page.getByRole("heading", {name: "Fixture Digest"})).toBeVisible();
});
