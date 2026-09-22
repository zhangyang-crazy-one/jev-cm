# jev-cm

一个 Pi 会话聊长了，前面的话会被收成一段摘要。摘要写得顺，却常常改了你原来的说法，接着又被当成已经发生的事。换到另一个项目，上一轮刚定下来的约定也像没说过。

jev-cm 接在 Pi 上。Jev 只负责判断：这段话现在还要不要留在眼前，这段工具输出还能不能先收起来。留下来的仍是你和助手当时写下的原话，放在你自己的电脑里。

你在甲项目里讲过一条退款规则，到乙项目里再问到相关的事，那句原话会跟着当前问题回来，并标明来自哪一次会话、哪个目录。同一件事不用再讲一遍。

工具一次吐出很长的日志时，眼下用不到的部分会先从眼前挪开，腾出位置。那一页原文还在，之后可以再打开。刚产生、还放得下的输出留在原地。会话最前面你写的说明和规则，压缩之后还是原来那些字，顺序也不变。

一轮说完，你写过的话和助手的回答会自己留下。命令列表里只有 `/jev`，没有单独的 remember。选中 `/jev` 之后，可以查看记下了多少句，也可以再把某一句原文钉住。

Jev 连不上、超时，或者还没设置密钥时，这一轮交给 Pi 自己的压缩。已经记下的对话还在。

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

在 Pi 里用 `/jev` 配置，结果写到 `~/.pi/agent/jev-cm.json`，权限 `0600`。压缩和 `jev-cm` 命令都会读这个文件；同名环境变量优先。命令列表里只有这一行。选中 `/jev` 之后再输入 `memory`，只显示数据库路径和条数。再输入 `remember` 和一句原文，会写入全局对话库，不经过 Jev。

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
