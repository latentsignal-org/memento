import {expect, test} from "@playwright/test";

test("root redirects to home and renders core navigation", async ({page}) => {
    await page.goto("/");

    await expect(page).toHaveURL(/\/home$/);
    await expect(page.getByRole("heading", {name: "Home"})).toBeVisible();
    await expect(page.getByRole("link", {name: "Memento", exact: true})).toBeVisible();
    await expect(page.getByRole("button", {name: /Ask Memento/i})).toBeVisible();

    await expect(page.getByRole("link", {name: "People", exact: true})).toBeVisible();
    await expect(page.getByRole("link", {name: "Projects", exact: true})).toBeVisible();
    await expect(page.getByRole("link", {name: "Newsletters", exact: true})).toBeVisible();
    await expect(page.getByRole("link", {name: "Concepts", exact: true})).toBeVisible();

    await expect(page.getByText("Archive Activity")).toBeVisible();
    await expect(page.getByRole("heading", {name: "Casey Rivera", exact: true})).toBeVisible();
});
