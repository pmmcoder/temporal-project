import { chromium } from "playwright-core";

const baseURL = "http://127.0.0.1:8080";
const title = `UI 审批 ${Date.now()}`;
const idempotencyKey = `approval-ui-${Date.now()}`;

const started = await requestJSON("/api/v1/workflows/approvaldemo/runs", {
  method: "POST",
  body: JSON.stringify({
    input: { title, summary: "从 WebUI 完成审批" },
    idempotencyKey
  })
});

const browser = await chromium.launch({
  executablePath: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  headless: true
});
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
const pageErrors = [];
page.on("pageerror", (error) => pageErrors.push(error.message));

await page.goto(`${baseURL}/approvals`, { waitUntil: "domcontentloaded" });
const card = page.locator("article.approval-card").filter({ hasText: title });
await card.waitFor();
await card.getByPlaceholder("填写审批备注").fill("WebUI smoke approved");
await page.screenshot({ path: "/private/tmp/workflow-approvals.png", fullPage: true });
await card.getByRole("button", { name: "通过" }).click();
await card.waitFor({ state: "detached" });

const deadline = Date.now() + 15_000;
while (Date.now() < deadline) {
  const detail = await requestJSON(
    `/api/v1/runs/${encodeURIComponent(started.workflowId)}/${encodeURIComponent(started.runId)}`
  );
  if (detail.status === "succeeded") {
    if (detail.output?.decision !== "approved") {
      throw new Error(`unexpected approval output: ${JSON.stringify(detail.output)}`);
    }
    if (pageErrors.length > 0) {
      throw new Error(`browser page errors: ${pageErrors.join("; ")}`);
    }
    console.log("approval WebUI smoke check passed");
    await browser.close();
    process.exit(0);
  }
  if (detail.status !== "running") {
    throw new Error(`approval workflow ended with status ${detail.status}`);
  }
  await new Promise((resolve) => setTimeout(resolve, 500));
}

throw new Error("approval workflow did not complete in time");

async function requestJSON(path, init = {}) {
  const response = await fetch(`${baseURL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Origin: baseURL,
      ...init.headers
    }
  });
  if (!response.ok) {
    throw new Error(`${path} returned HTTP ${response.status}: ${await response.text()}`);
  }
  return response.json();
}
