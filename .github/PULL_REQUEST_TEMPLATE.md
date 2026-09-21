## 改了什么、为什么

<!-- diff 已经说明了「改了什么」；这里解释「为什么必须改」。 -->

## 如何验证

<!-- 例如 `make check`，以及对运行中的服务做的手工 curl。 -->

- [ ] `make check` 通过（`fmt-check`、`vet`、带 `-race` 的离线测试、lint）

## 检查清单

- [ ] 行为变更配了离线测试（用 stub provider，不依赖网络）。
- [ ] 匹配逻辑的改动已用离线 stub provider 测试覆盖（含兜底遍历）。
- [ ] `/api/cover` 仍然是硬过滤，没有退化成「返回搜索结果第一条」。
- [ ] 若改了缓存身份或 TTL，`docs/SPEC.md` 第 7 节已同步。
- [ ] 用户可见的改动已更新 `CHANGELOG.md`。
- [ ] 参数、端点或兼容性有变动时已更新 `README.md`。

## 关联 issue

<!-- Closes #... -->
