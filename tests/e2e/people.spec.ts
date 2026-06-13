import {expect, test} from "@playwright/test";

test("people directory opens seeded person detail with masked email", async ({page}) => {
    await page.goto("/people");

    await expect(page.getByRole("heading", {name: "People Directory"})).toBeVisible();
    const personLink = page.getByRole("link", {name: /Casey Rivera/i}).first();
    await expect(personLink).toBeVisible();
    await personLink.click();

    await expect(page).toHaveURL(/\/people\/casey-rivera$/);
    await expect(page.getByRole("heading", {name: "Casey Rivera"})).toBeVisible();
    await expect(page.getByText("Frequent contact")).toBeVisible();
    await expect(page.getByText("Casey has been the main design partner for the Memento fixture project.")).toBeVisible();
    await expect(page.getByRole("button", {name: "ca***@example.com", exact: true})).toBeVisible();
    await expect(page.getByText("casey@example.com")).toHaveCount(0);
});
