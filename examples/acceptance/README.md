# 独立双机验收项目

零 npm 依赖的 Node 小项目，仅用于 Coordica 真实 Agent/双机验收。先把本目录复制到隔离目录并初始化独立 Git 仓库，不直接在 Coordica 工作树或安装目录运行开发任务。

基线函数与 `node --test test/greeting.test.mjs` 已可运行。新任务见 `task.json`：规范化名字的 Unicode 空白、保留文字、拒绝非字符串。允许修改 src/**、test/**；tools/** 的 owner verifier 不在允许范围。

`node tools/verify-acceptance.mjs` 在初始基线上应失败，开发完成后应通过。中心必须在固定候选 C 上运行它，不能仅接受 Agent 的自测总结。所有文件无模型密钥或中心凭据。

完整部署、检查参数、故障测试与实际项目开发顺序见安装包 `docs/two-machine-acceptance.md`。
