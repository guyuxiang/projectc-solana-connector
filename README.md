projectc-solana-connector
===================

`projectc-solana-connector` 是一个面向 Solana 的 HTTP API 与链上订阅服务。

它当前实现了两类能力：

- 基础链上查询与交易发送
- 基于 WebSocket + HTTP/RPC 的交易生命周期订阅

## Overview

服务对外暴露 HTTP 接口，对内通过以下链路处理订阅：

- WebSocket 订阅负责尽快发现新交易
- HTTP/RPC `getSignatureStatuses` 与 `getTransaction` 负责推进交易状态
- 数据库存储订阅状态、交易状态和 program 补拉 checkpoint

当前订阅语义为：

- `tx-subscribe`
  实际语义是订阅指定 `signature`
- `address-subscribe`
  接口字段名仍为 `address`，但在 Solana 上实际应传 `programId`

## Subscription Model

### Transaction Lifecycle

交易状态定义如下：

- `CONFIRMED`
  已确认，可进入大多数业务流程
- `FINALIZED`
  最终确认，适合高价值入账
- `DROPPED`
  被分叉丢弃 / 查询不到 / 超过订阅截止区块仍未出现
- `REVERTED`
  之前见过，后续链上状态消失或确认失败

状态推进路径通常为：

`CONFIRMED -> FINALIZED`

异常路径为：

- 从未真正上链且超过订阅窗口：`DROPPED`
- 已见过或已确认，后续链上状态消失：`REVERTED`

### Tx Subscribe

`tx-subscribe` 通过 Solana `signatureSubscribe` 监听指定签名。

- WebSocket 订阅 `confirmed` 级别消息
- 定时补拉 `getSignatureStatuses` / `getTransaction`
- 将状态推进到 `CONFIRMED` / `FINALIZED`
- 超过 `endBlockNumber` 仍未出现则标记 `DROPPED`

### Address Subscribe

`address-subscribe` 在 Solana 上实际用于订阅 `programId`。

- WebSocket 通过 `logsSubscribe` 监听 program 相关日志
- 收到日志后提取签名并立即 `getTransaction` 拉详情
- 定时补拉负责推进该签名的生命周期状态
- watcher 重连前会按 `lastObservedSlot` 回补断连窗口内遗漏的 program 交易
- 不再接受也不维护区块范围参数

注意：

- `address` 字段只是兼容已有接口命名
- 业务上应传入 `programId`

## WebSocket Reliability

当前 WebSocket 连接使用标准 `ping/pong` 心跳保活。

- 客户端固定周期发送 `ping`
- 收到 `pong` 后刷新 read deadline
- 如果超时收不到 `pong`，读循环会报错
- watcher 捕获错误后自动重连并重新订阅

这意味着当前实现不再依赖“无消息静默超时直接重连”，而是依赖真正的 WebSocket 心跳。

### Gap Backfill

program 订阅在每次启动 watcher 和每次重连前都会做一次 gap backfill：

- 调用 `getSignaturesForAddress(programId)` 分页拉取签名
- 使用 `limit` 控制单次批量
- 使用 `before` 继续翻页
- 使用 `until` 截止到上一次 checkpoint 签名
- 使用 `minContextSlot` 约束最小上下文 slot
- 再按签名调用 `getTransaction` 补详情

首次 watcher 启动如果还没有 checkpoint，不回扫全链历史，而是直接从当前最新 slot 开始跟踪未来。

## Callback Payload

交易状态回调消息体：

```json
{
  "state": "CONFIRMED",
  "previousState": "",
  "tx": {},
  "txEvents": []
}
```

说明：

- `state`: 当前交易状态
- `previousState`: 上一状态，首次回调为空
- `tx`: `DROPPED` / `REVERTED` 时可能为空
- `txEvents`: `DROPPED` / `REVERTED` 时可能为空

兼容性回滚消息体：

```json
{
  "txCode": "5Qx...",
  "networkCode": "solana",
  "state": "REVERTED",
  "previousState": "CONFIRMED"
}
```

## Key Config

主要配置位于 [`etc/GinApiServer.yaml`](/usr/src/golang/projectc-solana-connector/etc/GinApiServer.yaml)。

关键配置项：

- `connector.pollIntervalMs`
  定时补拉周期
- `connector.wsIdleTimeoutMs`
  WebSocket `pong` 超时时间
- `connector.retryBackoffMs`
  watcher 重连退避
- `connector.commitment`
  HTTP/RPC 查询使用的 commitment
- `networks.solana.endpoints`
  Solana HTTP RPC 地址
- `networks.solana.wsEndpoints`
  Solana WebSocket RPC 地址

## Storage

MySQL 中当前维护三类数据：

- `connector_tx_subscriptions`
  交易订阅
- `connector_address_subscriptions`
  program 订阅，包含 `last_observed_slot` 和 `last_observed_tx_code`
- `connector_published_states`
  交易生命周期状态

## Run

启动服务：

```bash
bash hack/start.sh
```

本地测试：

```bash
mkdir -p /tmp/projectc-go-cache
GOCACHE=/tmp/projectc-go-cache GOPROXY=off go test ./...
```

## Files

核心代码位置：

- [`pkg/service/subscription_service.go`](/usr/src/golang/projectc-solana-connector/pkg/service/subscription_service.go)
- [`pkg/service/chain_service.go`](/usr/src/golang/projectc-solana-connector/pkg/service/chain_service.go)
- [`pkg/solana/ws.go`](/usr/src/golang/projectc-solana-connector/pkg/solana/ws.go)
- [`pkg/store/subscription_store.go`](/usr/src/golang/projectc-solana-connector/pkg/store/subscription_store.go)
- [`Connector.md`](/usr/src/golang/projectc-solana-connector/Connector.md)
