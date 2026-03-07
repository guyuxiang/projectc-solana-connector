# 约定

Connector不面向集群外提供服务，所以内部无需实现鉴权逻辑

高内聚：链相关逻辑统一集成到connector中，除connector以外的其它服务应避免重复集成

本文中所有数据结构中的所有参数，如无特殊说明，均不能为空

# Connector功能模块

链实时交互模块

将链实时交互请求转换为节点服务调用逻辑，根据网络标识，在应用配置中心查询对应的网络配置获取节点地址

合约数据查询类的链实时交互，需要查询本地维护的合约信息，获取合约地址

链数据订阅模块，见《链数据订阅模块设计》

合约信息维护模块

模块内维护对应网络内部署的合约地址，合约ABI等信息

模块自动订阅当前的合约地址相关的交易，这意味着：

当新增合约地址时，调用订阅模块新增订阅

当变更合约地址时，调用订阅模块新增新地址的订阅，下线旧地址的订阅（通常来说，下线旧地址的方式为修改截止订阅范围为业务切换至新地址之后的某个区块号，以确保旧地址所有事件都将被完全同步）

# 通用数据结构

## 交易 ChainTx

{

\"code\": \"\", // 交易标识，一笔交易在指定网络内的唯一标识

\"networkCode\": \"polygon\", //
网络标识，标识交易所在的网络，与应用配置中心的公共网络编号配置保持一致

\"blockNumber\": 5, //
区块高度，标识交易在对应网络中的区块高度，格式为整型

\"timestamp\": 1745721784175, //
时间戳，标识交易出块的时间，格式为Unix毫秒级时间戳

\"status\": \"SUCCESS\", // 交易状态，SUCCESS/FAILED

\"from\": \"\", // from地址

\"to\": \"\", // to地址

\"amount\": \"\", // 金额

\"fee\": \"\", // 手续费

}

备注：

使用网络标识+交易标识，可以在多链架构中全局唯一确定一笔交易

## 事件 ChainEvent

{

\"code\": \"\", // 事件标识，一个事件在指定网络内的唯一标识

\"networkCode\": \"polygon\", //
网络标识，标识事件所在的网络，与应用配置中心的公共网络编号配置保持一致

\"blockNumber\": 5, //
区块高度，标识事件在对应网络中的区块高度，格式为整型

\"timestamp\": 1745721784175, //
时间戳，标识事件发生的时间，格式为Unix毫秒级时间戳

\"type\": \"xxx\", // 事件类型，详见【附录：链上事件】

\"data\": \"xxx\", // 事件数据，详见【附录：链上事件】

}

备注：

使用网络标识+事件标识，可以在多链架构中全局唯一确定一个事件

# Http API规范

## 链实时交互-写

URL格式：/inner/chain-invoke/{networkCode}/xxx

### 交易发送

向指定网络发送签名后的交易。

POST /inner/chain-invoke/{networkCode}/common/tx-send

// RequestBody

{

\"txSignResult\": \"xxx\", // 交易签名结果

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": {

\"txCode\": \"xxx\" // 交易标识

}

}

### 原生代币水龙头

使用connector内置的密钥凭证（如kms密码）签名，并发起指定网络的原生代币转让交易。

POST /inner/chain-invoke/{networkCode}/wallet/faucet

// RequestBody

{

\"acceptAddress\": \"xxx\", // 接收原生代币的地址\
\"idempotencyKey\": \"xxx\", // 幂等

"value\": 1.0, // float64

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": {

\"txCode\": \"xxx\" // 交易标识

}

}

## 链实时交互-读

URL格式：/inner/chain-data/{networkCode}/xxx

### 交易查询

根据交易标识，查询交易及其关联的事件。

POST /inner/chain-data/{networkCode}/common/tx-query

// RequestBody

{

\"txCode\": \"xxx\" // 交易标识

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": {

\"ifTxOnchain\": true, // 交易是否出块

\"tx\": {}, // ChainTx，交易不存在时为空

\"txEvents\": \[\], // ChainEvent array in the tx，交易不存在时为空

}

}

### 原生代币指定地址余额查询

查询指定网络指定地址的原生代币余额。

POST /inner/chain-data/{networkCode}/common/address-balance

// RequestBody

{

\"address\": \"\"

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": {

\"balance\": 0.5, // 余额，浮点型

\"balanceUnit\": \"xxx\", // 余额单位，详见【附录：余额单位】

}

}

### token发行量查询

查询指定token的实时总发行量。

POST /inner/chain-data/{networkCode}/common/token-supply

// RequestBody

{

\"tokenCode\": \"xxx\" // token业务标识

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": {

\"value\": 1.5 // 指定token总发行量，float64

}

}

### token指定地址持有量查询

查询指定token指定地址的实时持有量。

POST /inner/chain-data/{networkCode}/common/token-balance

// RequestBody

{

\"tokenCode\": \"xxx\", // token业务标识

\"address\": \"xxx\", // 指定地址

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": {

\"value\": 1.5 // 指定token总发行量，浮点型

}

}

### 

### 最新区块高度查询

查询指定网络的最新区块高度。

POST /inner/chain-data/{networkCode}/common/latest-block

// RequestBody

{}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": {

\"blockNumber\": 5, // 区块高度，格式为整型

\"timestamp\": 1745721784175, //
时间戳，标识区块出块的时间，格式为Unix毫秒级时间戳

}

}

## 订阅

URL格式：/inner/chain-data-subscribe/{networkCode}/xxx

### 订阅指定交易

订阅指定网络的指定交易。

POST /inner/chain-data-subscribe/{networkCode}/tx-subscribe

// RequestBody

{

\"txCode\": \"xxx\", // 交易标识

\"subscribeRange\": {

\"endBlockNumber\": 5, // 终止区块高度（闭区间）

}

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": null

}

### 订阅指定地址

订阅指定网络的指定地址的关联交易。

POST /inner/chain-data-subscribe/{networkCode}/address-subscribe

// RequestBody

{

\"address\": \"xxx\", // 指定地址

\"subscribeRange\": {

\"startBlockNumber\": 1, // 起始区块高度（闭区间）

\"endBlockNumber\": 5, // 终止区块高度（闭区间），可以为空

}

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": null

}

### 取消指定交易订阅

取消订阅指定网络的指定交易。

POST /inner/chain-data-subscribe/{networkCode}/tx-subscribe-cancel

// RequestBody

{

\"txCode\": \"xxx\", // 交易标识

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": null

}

### 取消指定地址订阅

取消订阅指定网络指定地址的关联交易。

POST /inner/chain-data-subscribe/{networkCode}/address-subscribe-cancel

// RequestBody

{

\"address\": \"xxx\", // 地址

\"endBlockNumber\": 10, // 终止区块高度（闭区间）

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": null

}

### 同步指定区块区间

POST /inner/chain-data-subscribe/{networkCode}/block-sync

// RequestBody

{

\"beginBlockNumber\": 5, // 地址，int64

\"endBlockNumber\": 10, // 终止区块高度（闭区间）

}

// ResponseBody

{

\"code\": \"200\",

\"message\": \"\",

\"data\": null

}

# 订阅回调消息体规范

## 交易出块回调

无论何种订阅，统一以该格式回调

{

\"tx\": {}, // ChainTx

\"txEvents\": \[\], // ChainEvent array in the tx

}

## 交易撤销回调

{

\"txCode\": \"\", // 交易标识

\"networkCode\": \"polygon\", // 网络标识

}

# 链数据订阅模块设计

## 

## 总体目标

订阅模块对外提供两种维度的订阅功能：地址订阅、交易订阅。

**地址订阅**：接收一个地址（必选）和一个区块范围（可选），订阅模块将保证：回调该区块范围内与该地址相关的所有交易。

**交易订阅**：接收一个交易标识（必选）和一个截止同步的区块高度（必选），订阅模块将保证：如该交易在截止同步的区块高度之前出块，回调该交易。

目前evm-connector的地址订阅基于eth_getLogs实现，故只支持合约地址的订阅。后续如果需要支持非合约地址的订阅，需要更换底层实现，例如切换至通过地址查询关联交易的API。

目前xrpl-connector的地址订阅基于手动筛选每个交易实现，支持所有地址的订阅。

## 关键思路

订阅模块的回调消息为交易维度，包含1交易+交易内的所有业务事件。

在解析和回调事件时，订阅模块会忽略不关心的事件，只回调业务关心的事件，即《附录：链上事件》中声明的事件。

将地址订阅转换为交易订阅，然后走统一的交易订阅逻辑。

保证在一次订阅请求接收后至少回调一次。

说明：业务系统中，关心同一交易的不同模块会出现触发订阅的时间差。例如，钱包模块发送交易后在时间点1发起订阅以监听交易状态，订阅模块在时间点2监听到交易并回调，业务模块在时间点3提交业务事务（上链中）。实际情况下，无法确保交易回调一定在业务模块事务提交后到达，当事务提交晚于交易回调时，业务系统将错过这次回调从而无法正确反转业务数据。为了避免这种情况，每个关心交易的模块都应该在己方确保能处理回调后再发起一次交易订阅，订阅模块保证在一次订阅请求接收后至少回调一次，即可确保"没有人会错过回调["]{dir="rtl"}。

模块应对自己回调的事件负责。

当链分叉等情况导致已回调内容变为分叉链上的数据时，模块应发送交易撤销的回调，这意味着模块需要维护自己已经同步并回调的数据，并通过机制保证同步的数据与主链数据的一致性

模块按照区块严格递增的方向检查已同步的订阅数据，并记录自己当前检查的checkpoint

本地有主链无的数据：删除并进行撤销回调

本地无主链有的数据：记录并进行回调

# 附录

## 链上事件

### DTT链上事件

类型-数据：

// RT_MINT

{

\"bid\": \"\", // 业务标识

\"tokenCode\": \"\", // token业务标识

\"recipient\": \"\", // 接收人地址

\"amount\": 1.0, // 金额

}

// RT_BURN

{

\"bid\": \"\", // 业务标识

\"tokenCode\": \"\", // token业务标识

\"owner\": \"\", // burn

\"amount\": 1.0, // 金额

}

// RT_TRANSFER

{

\"tokenCode\": \"\", // token业务标识

\"from\": \"\", // from地址

\"to\": \"\", // to地址

\"amount\": 1.0, // 金额

}

// RT_ENCASH

{

\"bid\": \"\", // 业务标识

\"tokenCode\": \"\", // token业务标识

\"encasher\": \"\", // encasher地址

\"amount\": \"\", // 金额

\"encashStatus\": \"\", // 业务状态, INIT/ACCEPT/REJECT

\"extension\": \"\",

}

// USER_PERMISSION_SET

{

\"bid\": \"\", // 业务标识

\"tokenCode\": \"\", // token业务标识

\"issuer\": \"\", // 发行方地址

\"user\": \"\", // 设置permission的user地址

\"permission\": 9999999, // 设置的permission

}

## 余额单位

----------------- -------------- -------------- ----------------------------
   **networkCode**   **代币币种**   **余额单位**     **与Credit的转换关系**

       polygon          MATIC        Wei, GWei,      1 Wei = 10^-9^ GWei =
                                       Ether         10^-18^ Ether = 10^-7^
                                                             Credit
    
        xrpl             XRP                      
    
       solana            SOL                      


​                                                  


----------------- -------------- -------------- ----------------------------

## 

## token业务标识

token业务标识与具体token的关系维护在connector内部。例如，evm-connector维护了token业务标识与网络、合约地址的关联关系。

标准格式：业务产品标识_symbol。

DTT_GLUSD

### 通用语言

规范：所有标识都是大小写敏感，不以数字开头，不包含空格

tokenCode ：token的唯一标识，手动定义，维护在业务层token表，通常为业务标识_tokenSymbol，例如 DTT_GLUSD

networkCode ：网络唯一标识，手动定义，维护在应用配置中心network表，例如 Polygon

contractCode ：合约标识（多网络共用），手动定义，维护在应用配置中心contract表

deployContractCode ：已部署合约实例的唯一标识，维护在connector，由部署行为触发生成，格式为 contractCode_networkCode，例如 DTT_GLUSD_Polygon, Dtt_DigitalTokenTradeDiamond_Polygon

约定1：token合约 contractCode=tokenCode

约定2：部署服务推送时，推送内容包含 networkCode 和 contractCode

约定3：connector内部维护deployContractCode的生成逻辑，应用合约部署推送时，生成deployContractCode；

约定4：connector收到来自业务层的token相关请求时，接收tokenCode与networkCode，即可根据约定3中维护的逻辑映射到已部署合约实例，获取地址、abi等合约信息
