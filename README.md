# jev-cm

Jev 负责判断，原文留在本机。库、命令行和测试都是 Go。Pi 加载 JavaScript 扩展，所以 `pi/index.js` 注册 `session_before_compact`，再调用 `jev-cm` 二进制。

## 产品特点

- **原文留在本机。** 对话、耐久记忆和被换出的工具输出都以原始字节写入本地 SQLite，默认 `~/.pi/agent/jev-cm.sqlite`。发给 Jev 的是待打分摘录，整库留在机器上。
- **跨项目记住对话。** 每个 Pi 项目和会话共用这一份库。一轮结束后写入用户消息和助手正文。工具结果和生成摘要不进对话库，工具输出仍走省略库。
- **下一轮带上相关原文。** 开跑前用当前问题检索全部项目。Jev 把达到 `0.5` 的原文按相关度放进系统提示的 `jev-memory` 一节，并标上会话和项目路径。这一节大约 2000 个估计 token，放不下的整段略过。库里没有候选时直接开跑。Pi 自己的系统提示保持原样。
- **压缩时换出低价值工具输出。** 分数低于默认 `0.25` 的旧工具输出在模型窗口里换成稳定指针，原文留在库里。达到 `0.25` 的保留原文。最近保护区内放得下的工具输出留在窗口里。消息条数和顺序保持不变。
- **开头的规则保持原字节。** 会话前缀里的指令和规则在压缩、重开窗口时保持原来的字节和顺序。前缀只在规则来源本身变化时变长。
- **按指针取回工具原文。** 展开已知指针时返回库里对应的那一页。
- **耐久内容单独过门槛。** 这类写入要 Jev 判断它值得长期保留、以后还有用，分数达到默认 `0.75` 才入库。读出时先做有限全文检索，再按当前请求打分，整段放进剩余 token 预算；放不下的整段记为 `over_budget`。`/jev remember` 直接保存原文。
- **打分失败时会话保持原样。** 认证失败、超时或结果无法使用时，现有消息留在原地，Pi 继续用自己的压缩。对话写入照常进行。召回打分失败时，`jev-memory` 留空，库里的行仍在。
- **两个提供方，同一套判断。** `typesafe` 和 `opencode-go` 都走 System One，问题类型是 Noul、Choice 和 Score。返回里带提供方、请求的模型名，以及提供方报告的模型版本。

## 构建

```bash
go build -o bin/jev-cm ./cmd/jev-cm
go test ./...
```

## 配置

- `JEV_CM_PROVIDER`：`typesafe` 或 `opencode-go`。旧名 `opencode-zen` 会按 `opencode-go` 加载
- 官方 key：`TYPESAFE_API_KEY`
- OpenCode Go key：`OPENCODE_GO_API_KEY`。没有单独设置时，使用控制台里的 `OPENCODE_API_KEY`
- `JEV_CM_MODEL`：官方可用 `jev-latest`，OpenCode Go 可用 `jev-1.13-free`
- `JEV_CM_SQLITE`：数据库路径
- `JEV_CM_DROP_THRESHOLD`：默认 `0.25`
- `JEV_CM_REMEMBER_THRESHOLD`：默认 `0.75`
- `JEV_CM_BIN`：插件调用的二进制路径。未设置时先找 `bin/jev-cm`，再找 `PATH` 里的 `jev-cm`

OpenCode Go 的 key 打到 Jev 的 System One 地址 `https://opencode.ai/zen/v1/systemone`。Go 目录里的 chat completions 地址不提供这个契约，配置成 System One 端点时会在加载时拒绝。

## Pi

在仓库里构建二进制后，用扩展文件启动：

```bash
go build -o bin/jev-cm ./cmd/jev-cm
pi -e ./pi/index.js
```

也可以 `pi install` 这个目录。`package.json` 的 `pi.extensions` 指向 `./pi/index.js`。改完扩展后重启 Pi，已安装的会话才会加载新命令。

在 Pi 里用 `/jev` 配置，结果写到 `~/.pi/agent/jev-cm.json`，权限 `0600`。压缩和 `jev-cm` 命令都会读这个文件；同名环境变量优先。`/jev memory` 只显示数据库路径和条数。`/jev remember` 把原文写入全局对话库，不经过 Jev。

```text
/jev
/jev provider typesafe
/jev provider opencode-go
/jev key
/jev model jev-latest
```

`/jev` 显示当前提供方、模型和 key 是否已设置，不打印完整 key。`/jev key` 会弹出输入框。`typesafe` 的模型是 `jev-1.13.0` 和 `jev-latest`。`opencode-go` 的模型是 `jev-1.13` 和 `jev-1.13-free`。

压缩开始时扩展读取即将被丢掉的消息，交给 `jev-cm prepare`。计划状态是 `compacted` 且带有原文或指针时，扩展把这些内容放进 Pi 要求的 `summary` 字段并交回。Pi 0.84.2 收到这个结果后不再调用自己的摘要模型。字段名是宿主的存储槽，内容是指针和原文，不是模型写的摘要。Jev 失败、二进制缺失，或计划没有可注入内容时，扩展不返回结果，Pi 继续用自己的压缩。

## 卸载

从 Pi 扩展配置里去掉这个扩展，然后重启 Pi。这不会删除 SQLite。

只有显式带上 `--yes` 才会删库：

```bash
jev-cm purge --yes
```
