import { test, expect } from "@playwright/test";

test.describe("RepoAtlas E2E User Flows", () => {
  test("1. Offline-capable full E2E flow (URL input -> async analysis -> node selection -> details -> search/directory filter -> demo mode)", async ({
    page,
  }) => {
    // 1. トップ画面を開く
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "RepoAtlas" })
    ).toBeVisible();

    // 2. GitHub URLを入力する (外部GitHub接続に依存しないfixtureリポジトリ)
    const urlInput = page.getByLabel("GitHub Public Repository URL (Python)");
    await urlInput.fill("https://github.com/fixture/python-small");
    await page.getByRole("button", { name: "依存グラフを解析" }).click();

    // 3. 解析が完了し、グラフ画面が表示されるのを待つ
    await expect(page).toHaveURL(/\/analysis\/[0-9a-f-]+/);
    await expect(
      page.getByRole("link", { name: "fixture/python-small" })
    ).toBeVisible({ timeout: 30_000 });

    // 解析サマリーの検証 (4 files, 4 internal edges)
    await expect(page.locator(".toolbar-metrics")).toContainText("解析ファイル:");
    await expect(page.locator(".toolbar-metrics")).toContainText("4");

    // 4. グラフ上のノードを選択する (FileNodeをクリック)
    const serviceNode = page.getByTestId("graph-node-app/service.py");
    await expect(serviceNode).toBeVisible();
    await serviceNode.click();

    // 5. ファイル詳細パネルの表示を検証する
    const detailsPanel = page.getByLabel("ファイル詳細パネル");
    await expect(detailsPanel).toContainText("app/service.py");
    await expect(
      detailsPanel.getByRole("link", { name: /GitHubでソースコードを開く/ })
    ).toHaveAttribute(
      "href",
      /https:\/\/github\.com\/fixture\/python-small\/blob\/[0-9a-f]{40}\/app\/service\.py/
    );

    // 6. 検索・ディレクトリフィルタを操作する
    const searchInput = page.getByLabel("ファイルパス検索");
    await searchInput.fill("models");
    await expect(page.getByTestId("graph-node-app/models.py")).toBeVisible();
    await expect(page.getByTestId("graph-node-app/service.py")).toHaveCount(0);

    // 検索リセットとディレクトリフィルタの操作
    await page.getByRole("button", { name: "リセット" }).click();
    await expect(page.getByTestId("graph-node-app/service.py")).toBeVisible();

    const dirSelect = page.getByLabel("ディレクトリフィルタ");
    await dirSelect.selectOption("app");
    await expect(page.getByTestId("graph-node-app/main.py")).toBeVisible();

    // Phase 2-A: Guided Code Tour (Next / Previous navigation & reason display)
    const tourBar = page.getByTestId("guided-tour-bar");
    await expect(tourBar).toBeVisible();
    await expect(page.getByTestId("tour-current-reason")).toContainText("推奨理由:");
    await page.getByTestId("tour-next-btn").click();
    await expect(page.getByTestId("tour-current-path")).toContainText("Step 2:");
    await page.getByTestId("tour-prev-btn").click();
    await expect(page.getByTestId("tour-current-path")).toContainText("Step 1:");

    // Phase 2-B: Impact Analysis (select app/models.py and verify BFS dependents + depth control + disclaimer)
    await page.getByTestId("graph-node-app/models.py").click();
    const impactSection = page.getByTestId("impact-analysis-section");
    await expect(impactSection).toBeVisible();
    await expect(page.getByTestId("impact-total-count")).toHaveText("2");
    await expect(page.getByTestId("impact-direct-count")).toHaveText("2");
    await expect(page.getByTestId("impact-disclaimer")).toContainText(
      "静的import依存からBFSで推定した潜在的影響範囲"
    );
    await page.getByTestId("impact-depth-select").selectOption("1");
    await expect(page.getByTestId("impact-total-count")).toHaveText("2");

    // Phase 2-C: Architecture View (toggle to Architecture View -> inspect directory group -> click internal file)
    await page.getByTestId("view-mode-architecture").click();
    const archDirNode = page.getByTestId("arch-node-app");
    await expect(archDirNode).toBeVisible();
    await archDirNode.click();
    await expect(page.getByTestId("arch-details-panel")).toBeVisible();
    await expect(page.getByTestId("arch-file-count")).toHaveText("4");
    await page.getByTestId("arch-file-item-app/main.py").click();
    await expect(page.getByTestId("graph-node-app/main.py")).toBeVisible();

    // 7. デモモードを開く
    await page.goto("/");
    await page.getByRole("link", { name: "デモを開く" }).click();
    await expect(page).toHaveURL(/\/demo/);
    await expect(page.getByTestId("demo-badge")).toBeVisible();
    await expect(
      page.getByRole("link", { name: "repoatlas-demo/python-web-starter" })
    ).toBeVisible();
  });

  test("2. Real GitHub E2E verification (pallets/itsdangerous & psf/requests + cache hit)", async ({
    page,
    request,
  }) => {
    test.skip(
      process.env.RUN_REAL_GITHUB_E2E !== "1",
      "Set RUN_REAL_GITHUB_E2E=1 to run against live GitHub API"
    );

    const apiBase =
      process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080";

    for (const repoUrl of [
      "https://github.com/pallets/itsdangerous",
      "https://github.com/psf/requests",
    ]) {
      await page.goto("/");
      const urlInput = page.getByLabel("GitHub Public Repository URL (Python)");
      await urlInput.fill(repoUrl);
      await page.getByRole("button", { name: "依存グラフを解析" }).click();

      await expect(page).toHaveURL(/\/analysis\/[0-9a-f-]+/);
      const currentUrl = page.url();
      const analysisId = currentUrl.split("/analysis/")[1];

      const shortRepo = repoUrl.replace("https://github.com/", "");
      await expect(
        page.getByRole("link", { name: shortRepo })
      ).toBeVisible({ timeout: 60_000 });

      // Click the #1 recommended file chip and verify details panel updates
      const topRecChip = page.locator(".recommended-bar .chip-btn").first();
      await expect(topRecChip).toBeVisible();
      await topRecChip.click();

      const detailsPanel = page.getByLabel("ファイル詳細パネル");
      await expect(
        detailsPanel.getByRole("link", { name: /GitHubでソースコードを開く/ })
      ).toBeVisible();

      // Verify re-analyzing the same repository returns HTTP 200 with cached=true and same analysis_id
      const cacheRes = await request.post(`${apiBase}/api/v1/analyses`, {
        data: { repository_url: repoUrl },
      });
      expect(cacheRes.status()).toBe(200);
      const cacheJson = await cacheRes.json();
      expect(cacheJson.cached).toBe(true);
      expect(cacheJson.analysis_id).toBe(analysisId);
      expect(cacheJson.status).toBe("completed");
    }
  });

  test("3. Multi-viewport UI/UX verification (1440px, 768px, 375px), zero console errors, no overflow, and screenshots", async ({
    page,
  }) => {
    const runtimeErrors: string[] = [];
    page.on("pageerror", (err) => runtimeErrors.push(err.message));
    page.on("console", (msg) => {
      if (msg.type() === "error") {
        runtimeErrors.push(msg.text());
      }
    });

    // Desktop 1440px
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "RepoAtlas" })
    ).toBeVisible();

    const overflow1440Home = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth
    );
    expect(overflow1440Home).toBe(false);
    await page.screenshot({ path: "../docs/images/home-1440.png" });

    await page.goto("/demo");
    await expect(page.getByTestId("guided-tour-bar")).toBeVisible();
    await expect(page.getByTestId("graph-legend")).toBeVisible();
    await page.getByRole("button", { name: "#3 app/db.py" }).click();
    await expect(page.getByTestId("impact-total-count")).toHaveText("5");
    const overflow1440Graph = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth
    );
    expect(overflow1440Graph).toBe(false);
    await page.screenshot({ path: "../docs/images/graph-file-view-1440.png" });

    await page.getByTestId("view-mode-architecture").click();
    await expect(page.getByTestId("arch-node-app")).toBeVisible();
    await page.screenshot({ path: "../docs/images/graph-arch-view-1440.png" });
    await page.getByTestId("view-mode-file").click();

    // Tablet 768px
    await page.setViewportSize({ width: 768, height: 1024 });
    await page.goto("/demo");
    await expect(page.getByTestId("guided-tour-bar")).toBeVisible();
    const overflow768 = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth
    );
    expect(overflow768).toBe(false);

    // Mobile 375px
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto("/");
    const overflow375Home = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth
    );
    expect(overflow375Home).toBe(false);

    await page.goto("/demo");
    await expect(page.getByTestId("guided-tour-bar")).toBeVisible();
    const overflow375Graph = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth
    );
    expect(overflow375Graph).toBe(false);

    // Switch to details tab on mobile 375px and verify Impact Analysis is operable
    await page.getByRole("button", { name: /ファイル詳細/ }).click();
    await expect(page.getByTestId("impact-analysis-section")).toBeVisible();
    await page.screenshot({ path: "../docs/images/graph-mobile-375.png" });

    expect(runtimeErrors).toEqual([]);
  });
});
