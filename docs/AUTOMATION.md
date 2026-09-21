# 仓库自动化与设置

这份文档记录仓库的自动化流程，以及那些**无法随代码版本化**、只能在 GitHub 网页端设置的项目。前者看代码就知道，后者只能靠记，所以更需要写下来。

> **当前状态**：项目已在本地初始化完成，GitHub 仓库尚未创建（名字已确认可用：`x-cyber-space/x-cyber-cover-hub`）。工作流文件已就位，但下面第 3 节的网页端设置**需要在仓库创建后逐项应用**。

---

## 1. 工作流一览

| Workflow | 触发条件 | 做什么 |
|---|---|---|
| `ci.yml` | push 到 main、pull request | gofmt、build、vet、`go test -short -race`、golangci-lint、4 平台交叉编译，以及容器镜像的**构建 + 启动冒烟测试** |
| `codeql.yml` | push/PR 到 main、每周一 | Go 的 CodeQL `security-and-quality` 分析 |
| `dependency-review.yml` | pull request | PR 引入中危及以上的依赖漏洞时直接失败 |
| `scorecard.yml` | push 到 main、每周一 | OpenSSF Scorecard，发布结果并上传 SARIF |
| `release.yml` | `v*` 标签、手动 dispatch | `make check-core` → GoReleaser 归档 → 多架构镜像推送到 GHCR |

若手动 dispatch `release.yml` 并勾选 `snapshot`，会走一遍完整构建但**不发布**、也**不推镜像**——这是验证发布链路的标准方式：

```bash
gh workflow run release.yml -f snapshot=true
```

---

## 2. 依赖更新

`dependabot.yml` 覆盖三个生态：Go 模块、GitHub Actions、Docker 基础镜像。

第三方 action 一律按 **commit SHA** 固定，版本号写在行尾注释里（`# v7.0.1`）。Dependabot 会同时更新 SHA 和注释——所以固定 SHA 并不会让版本停在原地。

> GitHub Actions 的"大版本"和普通库的 semver 不是一回事。多数大版本升级的真实原因是 **GitHub 淘汰旧 Node 运行时**（node16 → node20 → node24），而不是 API 重写。因此 Dependabot 对 Actions 默认允许跨大版本升级——不升就永远拿不到运行时修复。

---

## 3. 无法版本化的网页端设置

这些只能在 Settings → … 里点，重建仓库时必须重新设置一遍。以下均已应用到本仓库；说明部分写的是**漏掉会坏成什么样**。

### Actions → General

把 `GITHUB_TOKEN` 默认权限设为 **read-only**，并限制允许的 action。两个坑，都是实际踩过的：

1. **`verified_allowed` 覆盖不全**。`golangci/golangci-lint-action` 并不是 GitHub 认定的 "verified creator"，所以只勾"GitHub 官方 + 已验证发布者"会让 CI 工作流**直接启动失败**，报 `startup_failure`，而且**没有任何注解说明原因**。同理，四个 `docker/*` action 和 `goreleaser/goreleaser-action` 也加入了显式放行名单。宁可维护一份精确的 `patterns_allowed`，也不要把策略放宽成 `all`。

2. **`sha_pinning_required` 只有在所有 `uses:` 都已是 40 位 SHA 时才能打开**。一旦引入一个用 tag 引用的 action，工作流会在**启动阶段**失败，而不是在某个步骤里失败。

`actionlint` 能校验工作流**文件**的语法，但这两条都属仓库策略而非语法，它查不出来。

### Rulesets

**main 分支**的规则集要求：

- 必须走 Pull Request
- 9 项必需状态检查（见下）
- 线性历史
- 禁止 force push、禁止删除
- 对话必须已解决
- 只允许 squash 合并
- 管理员保留绕过权限（应急用，不该当日常路径）

**`v*` 标签**的规则集禁止移动和删除标签——release 是绑定到具体 tag 的。

> ⚠️ **只能把「在 PR 上确实会运行」的检查列为必需。** `scorecard analysis` 只在 push 到 main 和定时触发时运行，**从不在 PR 上运行**；一旦列为必需，每个 PR 都会永久卡在 "Expected — Waiting for status to be reported"。同理，容器冒烟测试**故意不加 `paths:` 过滤**——会跳过自己的必需检查等于让那些 PR 永久等待。

目标必需检查（与姊妹项目 `x-cyber-lrc-hub` 保持一致）：

```
build, vet and test                cross-compile (windows/amd64)
golangci-lint                      analyze (go)
cross-compile (linux/amd64)        review dependency changes
cross-compile (linux/arm64)        build and run container image
cross-compile (darwin/arm64)
```

### Code security

- **Dependency graph**：必须**最先**开启。它**没有 REST API**，只能网页端点；没开的话 `dependency-review.yml` 会在每个 PR 上失败，报 "Dependency review is not supported on this repository"。
- **Dependabot security updates**：与 `dependabot.yml` 配置的版本更新是**两个独立开关**。
- **Secret scanning + push protection**：公开仓库免费。（`non-provider patterns` 和 `validity checks` 属于付费的 GitHub Secret Protection，公开仓库免费额度不含这两项。）
- **Private vulnerability reporting**：必须开——`SECURITY.md` 里就是让报告者走这条路。

### Pull requests

只允许 squash 合并、合并后自动删除分支。这两条规则集里也强制了，网页端设置只是兜底。

---

## 4. 本地对应的命令

CI 跑的检查，本地都能跑：

```bash
make check        # fmt-check + vet + test + lint（需要 golangci-lint）
make check-core   # 不需要任何额外工具的版本
make release-check # 校验 .goreleaser.yml
```

`release.yml` 用的是 `check-core` 而不是 `check`：GitHub runner 上没有预装 golangci-lint，而 lint 已由 CI 的独立 job 把关——标签必然指向一个过了 CI 的提交。

> 本地用 docker 跑 `goreleaser release --snapshot` 会在 `dist/` 留下 **root 属主**的文件。本地一般只做 `release-check`，真正的打包交给 CI。
