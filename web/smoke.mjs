import { chromium } from "playwright-core";

const executablePath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const browser = await chromium.launch({ executablePath, headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const pageErrors = [];

page.on("pageerror", (error) => pageErrors.push(error.message));

await page.goto("http://127.0.0.1:18080/runs", { waitUntil: "domcontentloaded" });
await page.getByText("greeting", { exact: true }).first().waitFor();
await page.screenshot({ path: "/private/tmp/workflow-runs.png", fullPage: true });

await page.getByText("greeting:web-smoke-002", { exact: true }).click();
await page.getByText("进行中的 Activity", { exact: true }).waitFor();
await page.getByText("Hello Ada", { exact: false }).waitFor();
await page.getByText("在 Temporal UI 中查看", { exact: false }).waitFor();
await page.screenshot({ path: "/private/tmp/workflow-detail.png", fullPage: true });

await page.goto("http://127.0.0.1:18080/workflows", { waitUntil: "domcontentloaded" });
await page.getByText("并行问候", { exact: true }).first().waitFor();
await page.screenshot({ path: "/private/tmp/workflow-catalog.png", fullPage: true });

await page.setViewportSize({ width: 390, height: 844 });
await page.goto("http://127.0.0.1:18080/runs", { waitUntil: "domcontentloaded" });
await page.getByText("运行", { exact: true }).first().waitFor();
await page.screenshot({ path: "/private/tmp/workflow-runs-mobile.png", fullPage: true });

if (pageErrors.length > 0) {
  throw new Error(`browser page errors: ${pageErrors.join("; ")}`);
}

console.log("desktop and mobile WebUI smoke check passed");
await browser.close();
