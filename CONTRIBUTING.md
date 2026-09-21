# 贡献指南

感谢你有兴趣改进 `x-cyber-cover-hub`。这份文档只讲实用的部分：怎么构建、要过哪些检查，以及几条**容易搞错的项目专属规矩**。

---

## 环境准备

```bash
git clone https://github.com/x-cyber-space/x-cyber-cover-hub.git
cd x-cyber-cover-hub

make help     # 列出所有目标
make build    # 产物在 bin/x-cyber-cover-hub
make check    # 一次跑完 CI 的全部检查：fmt-check、vet、test、lint
```

依赖：`Go 1.25+`；建议再装 `golangci-lint` v2：

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

`make lint` 会先找 PATH，找不到则回退到 `$(go env GOPATH)/bin`。

---

## 日常开发循环

`main` 有规则集保护，**`git push origin main` 会被直接拒绝**。所有改动都要走分支 + PR。

下面的命令**不依赖任何本地 git 配置**：

```bash
# 1. 开工前同步
git switch main
git pull --ff-only

# 2. 建分支
git switch -c feat/better-album-match

# 3. 改代码，本地自测
make check

# 4. 提交
git commit -am "feat: ..."

# 5. 推送并建立跟踪关系
git push -u origin feat/better-album-match

# 6. 开 PR
gh pr create --fill

# 7. 等检查并合并
gh pr checks --watch
gh pr merge --squash --auto

# 8. 回到 main
git switch main
git pull --ff-only
git branch -D feat/better-album-match
```

`gh pr merge --squash --auto` 开启自动合并：必需检查一绿就自动合并，你不必回来点。

### 为什么第 5 步要写 `-u origin <分支名>`

`--ff-only` 与 `-u` 都是**显式形式**，目的是让行为不依赖任何本地配置或 git 的推断：

- `--ff-only` 保证 `main` 只会快进。若你本地设了 `pull.rebase=true`，裸 `git pull` 会走 rebase。
- `-u origin <分支名>` 不依赖 `push.default` 的取值（若为 `nothing`，裸 `git push` 会直接报错）。

### 几条容易踩的

- **只允许 squash 合并**，所以分支里有多少个「改错了」的提交都无所谓。
- **清理本地分支必须用 `-D`，不能用 `-d`。** squash 合并产生的是**全新提交**，不是分支提交的后代，所以 `git branch -d` 的「是否已合并」检查**必然失败**（`not fully merged`）——而内容其实已经合进去了。同理 `git branch --merged main` 也不会列出已 squash 合并的分支。

  安全的一次性清理（以「远端分支已被删除」为判据，与输出语言环境无关）：

  ```bash
  git fetch --prune
  git for-each-ref --format='%(refname:short)|%(upstream)' refs/heads | while IFS='|' read -r b u; do
    [ -z "$u" ] && continue
    git rev-parse -q --verify "$u" >/dev/null 2>&1 || git branch -D "$b"
  done
  ```

- **不要在分支上 `git merge main`**；要同步就用 rebase：`git pull --rebase origin main`。
- **同一 PR 连续推送时，上一次还在跑的 CI 会被取消**（`concurrency` 配置），这是有意的。

### 可选的本地配置

```bash
git config --global fetch.prune true   # fetch 时自动清理已删除远端分支的跟踪引用
```

只有这一条是真正推荐的：不设它，`git branch -a` 会一直列着早已在远端消失的分支。

`pull.ff only` 与 `push.autoSetupRemote` 不是必需的——现代 git 遇到分叉本来就会拒绝而不是静默合并，而新建分支上裸 `git push` 也会自动建立跟踪关系（实测于 git 2.43，设与不设无差别）。

---

## 测试

```bash
make test       # 离线，带 -race。CI 跑的就是这个
make test-all   # 额外包含真实上游冒烟测试，需要外网
```

匹配、缓存、调度器的兜底行为、HTTP 层——全部有离线覆盖，由 stub provider 驱动。**改了行为就要补离线测试。**

---

## 本项目的几条硬规矩

下面每一条都是真实 bug 留下的教训。

### 绝不返回"搜索结果第一条"

消费方会把拿到的字节**写进音频文件的 ID3/FLAC 标签**，那是永久的。错封面要人工去改，而 `404` 是可恢复的——调用方可以换关键词重试，或让用户手选。

`internal/matching` 是**闸门**（是不是这张专辑），`ScoreCover` 是**排序**（哪张更像）。专辑一项满分 60，本身就能过任何总分阈值——这正是它**绝不能**单独决定接受与否的原因。

### 缓存不能回答冷路径会拒绝的查询

每次缓存命中都用**与冷启动相同的谓词**重新校验（`cache.Matches`）。新增任何读缓存的代码路径，都必须跑这个谓词。

身份只在 `matching.IdentityKey` 里定义**一次**，缓存键与结果去重共用。不要在第二个地方重新计算。

### 去重键必须带上 source

`dedupe` 的键是 `(source, artist, album)`，不是 `(artist, album)`。若少了 source，iTunes 的条目与网易云的条目会被合并；主源先试且它可能没图，**兜底就永远走不到**。这是实测出来的 bug，`TestResolveTriesEachSourceOncePerAlbum` 守着它。

### 上游故障不等于"没有这张专辑"

所有源都失败是 `503`，不是 `404`。把故障伪装成"没有封面"会让用户以为自己的元数据有问题。

### 相似度比较前必须先剥离版次括号

`NormalizeForSearch` 要先 `bracketRegex` 再删非字母数字字符。否则 `(Deluxe Edition)` 会残留成字母 `deluxeedition`，把本来完全相同的专辑压到满分以下——**版次标记的含义恰好相反**。

---

## 提交与 Pull Request

- 提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/)：`feat:`、`fix:`、`docs:`、`refactor:`、`test:`、`chore:`。
- **正文写清「为什么」**。
- `go.mod`、`go.sum` 和 `CHANGELOG.md` 要跟代码改动保持一致。
- 开 PR 之前跑一遍 `make check`。

---

## 代码风格

- `gofmt` 强制。`golangci-lint` 的检查集**刻意保持很小**——目的是抓错误，不是堆积个人偏好。
- **包名要描述它所属的领域。** 项目里没有 `util`、`common`、`helpers` 这类包。
- 优先写「解释某个不显然的约束」的注释，而不是「复述代码」的注释。

---

## 仓库自动化

工作流清单、Dependabot 配置，以及那些**只能网页端设置、无法版本化**的项目，全部记录在 [`docs/AUTOMATION.md`](docs/AUTOMATION.md)。
