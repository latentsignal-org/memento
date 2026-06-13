import {expect, test} from "@playwright/test";

test("projects index opens seeded project detail", async ({page}) => {
    await page.goto("/projects");

    await expect(page.getByRole("heading", {name: "Project"})).toBeVisible();
    const projectLink = page.getByRole("link", {name: /Fixture Launch/i}).first();
    await expect(projectLink).toBeVisible();
    await projectLink.click();

    await expect(page).toHaveURL(/\/projects\/fixture-launch$/);
    await expect(page.getByRole("heading", {name: "Fixture Launch"})).toBeVisible();
    await expect(page.getByText("3 messages").first()).toBeVisible();
    await expect(page.getByText("Casey Rivera").first()).toBeVisible();
    await expect(page.getByText("Fixture Launch coordinates the E2E harness rollout").first()).toBeVisible();
    await expect(page.getByRole("heading", {name: /Phase I: Harness foundation/i})).toBeVisible();
    await expect(page.getByText("The project is ready to validate one simulated generation workflow").first()).toBeVisible();
    await expect(page.getByText("The team deferred broader dimension coverage")).toBeVisible();
    await expect(page.getByText("Supporting Emails (3)")).toBeVisible();
});
