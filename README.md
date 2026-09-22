# jev-cm

Pi 的图引导动态上下文。Jev 只负责判断：哪段原话和当前问题相关、哪条工具输出可以先收起来。留下来的都是你和助手当时写下的原话，存在你自己的电脑里。

## 现在的效果

- **对话按工作线连成图。** 每轮对话原样入库后，Jev 判断新原话和最多八条相似旧原话是否属于同一条工作线，达到 `0.5` 就存一条 `related` 边。
- **召回：检索给种子，图扩出邻居，Jev 做裁决。** 全文检索（汉字按相邻两字切词）取出种子，沿边双向走最多两跳，Jev 对每条候选按当前问题打分，达到 `0.5` 才放进会话。实测：和查询零共同词的段落，沿图被 Jev 以 0.64 放行——它认的是同一条工作线，不是词面重叠。
- **压缩对着宿主窗口做，不写摘要。** 压力线 = 模型上下文窗口 − Pi 的 `reserveTokens`。待压缩部分达到这条线的 `0.65` 时，Jev 逐条评估受保护尾部以外的工具输出：低于 `0.25` 的原文收进 SQLite，窗口里只留一行指针，之后按页取回。新窗口由冻结前缀、指针和图召回的原文组成，没有模型改写的摘要。评估自动分批，每批不超过 Jev 的 32K token 上限，大会话也能跑完。
- **失败都向后退。** Jev 连不上、超时或没密钥时：召回不注入，压缩交回 Pi 自己做。已经记下的原话和边都在。

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
- `JEV_CM_RECALL_CUTOFF`：召回分数线，默认 `0.5`
- `JEV_CM_SHORTLIST`：召回候选条数，默认 `20`
- `JEV_CM_BIN`：插件调用的二进制路径。未设置时先找 `bin/jev-cm`，再找 `PATH` 里的 `jev-cm`

OpenCode Go 的 key 打到 Jev 的 System One 地址 `https://opencode.ai/zen/v1/systemone`。Go 目录里的 chat completions 地址不提供这个契约，配置成 System One 端点时会在加载时拒绝。

## Pi

在仓库里构建二进制后，用扩展文件启动：

```bash
go build -o bin/jev-cm ./cmd/jev-cm
pi -e ./pi/index.js
```

也可以 `pi install` 这个目录。`package.json` 的 `pi.extensions` 指向 `./pi/index.js`。改完扩展后重启 Pi，已安装的会话才会加载新命令。

在 Pi 里用 `/jev` 配置，结果写到 `~/.pi/agent/jev-cm.json`，权限 `0600`。压缩和 `jev-cm` 命令都会读这个文件；同名环境变量优先。

```text
/jev
/jev provider typesafe
/jev provider opencode-go
/jev key
/jev model jev-latest
```

`/jev` 显示当前提供方、模型和 key 是否已设置，不打印完整 key。`/jev key` 会弹出输入框。`typesafe` 的模型是 `jev-1.13.0` 和 `jev-latest`。`opencode-go` 的模型是 `jev-1.13` 和 `jev-1.13-free`。

## 使用

全局对话在 `~/.pi/agent/jev-cm.sqlite`。新开一个会话，或压缩之后眼前只剩摘要，这个文件还在。`/jev memory` 只显示路径和条数。

`/jev remember` 把当前会话里你和助手的原话写入这个库，不调用 Jev、不建边。当前会话没有这类原话时，提示「这段对话里还没有可留下的内容」。

`/jev recall` 后面不写字时，用当前会话里你和助手的原话当检索词。当前会话没有这类原话时，提示「当前对话里还没有可对照的内容」。要直接按一句话查全局库，把这句话写在命令后面：

```text
/jev recall jev 插件
```

检索按当前效果一节的三步走：切词做全文检索（`jev插件开发` 会拆成 `jev`、`插件`、`开发` 去查），沿图走两跳，Jev 打分。候选为空时不调用 Jev，提示「没有对得上的原文」。达到分数线的原文整段放进当前会话，标明会话 id 和目录，放得进约 2000 token；放不进的整段省略，低于分数线的留在库里。Jev 连不上、超时或没密钥时，候选标成未打分，同样不放进会话。这条命令不会把库里的全部原文塞进当前会话。

下一轮你直接提问时，用这句提问走同一条路径。对得上的原文进 `jev-memory`。

## 卸载

从 Pi 扩展配置里去掉这个扩展，然后重启 Pi。这不会删除 SQLite。

只有显式带上 `--yes` 才会删库：

```bash
jev-cm purge --yes
```
