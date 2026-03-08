package config

type Config struct {
	Server       *Server                    `yaml:"server"`
	Auth         *Auth                      `yaml:"auth"`
	Gin          *Gin                       `yaml:"gin"`
	Log          *Log                       `yaml:"log"`
	Connector    *Connector                 `yaml:"connector"`
	Networks     map[string]*SolanaNetwork  `yaml:"networks"`
	Tokens       map[string]*Token          `yaml:"tokens"`
	ContractInfo map[string]*ContractBundle `yaml:"contractInfo"`
}

type Server struct {
	Port    int    `yaml:"port"`
	Host    string `yaml:"host"`
	Version string `yaml:"version"`
}

type Auth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Gin struct {
	Mode string `yaml:"mode"`
}

type Log struct {
	Level string `yaml:"level"`
}

type Connector struct {
	RequestTimeoutMs   int                      `yaml:"requestTimeoutMs"`
	RetryTimes         int                      `yaml:"retryTimes"`
	RetryBackoffMs     int                      `yaml:"retryBackoffMs"`
	Commitment         string                   `yaml:"commitment"`
	PollIntervalMs     int                      `yaml:"pollIntervalMs"`
	ReorgDepth         uint64                   `yaml:"reorgDepth"`
	TxSubscribeWindow  uint64                   `yaml:"txSubscribeWindow"`
	IdempotencyTtlSec  int                      `yaml:"idempotencyTtlSec"`
	SubscriptionBuffer int                      `yaml:"subscriptionBuffer"`
	Callback           *CallbackConfig          `yaml:"callback"`
	SubscriptionStore  *SubscriptionStoreConfig `yaml:"subscriptionStore"`
}

type CallbackConfig struct {
	Mode                string `yaml:"mode"`
	URL                 string `yaml:"url"`
	Host                string `yaml:"host"`
	Port                int    `yaml:"port"`
	Username            string `yaml:"username"`
	Password            string `yaml:"password"`
	VirtualHost         string `yaml:"virtualHost"`
	VirtualHostLegacy   string `yaml:"virtual-host"`
	Exchange            string `yaml:"exchange"`
	ExchangeType        string `yaml:"exchangeType"`
	RoutingKey          string `yaml:"routingKey"`
	Durable             bool   `yaml:"durable"`
	Mandatory           bool   `yaml:"mandatory"`
	Persistent          bool   `yaml:"persistent"`
	Confirm             bool   `yaml:"confirm"`
	ReconnectIntervalMs int    `yaml:"reconnectIntervalMs"`
}

type SubscriptionStoreConfig struct {
	MySQL *MySQLConfig `yaml:"mysql"`
}

type MySQLConfig struct {
	DSN            string `yaml:"dsn"`
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Database       string `yaml:"database"`
	MaxOpenConns   int    `yaml:"maxOpenConns"`
	MaxOpenConnsV2 int    `yaml:"maxOpenconns"`
	MaxIdleConns   int    `yaml:"maxIdleConns"`
	ConnMaxLifeSec int    `yaml:"connMaxLifeSec"`
}

type SolanaNetwork struct {
	Code             string            `yaml:"code"`
	ChainID          string            `yaml:"chainId"`
	NativeSymbol     string            `yaml:"nativeSymbol"`
	LamportsPerToken uint64            `yaml:"lamportsPerToken"`
	Endpoints        []string          `yaml:"endpoints"`
	WsEndpoints      []string          `yaml:"wsEndpoints"`
	Faucet           *FaucetConfig     `yaml:"faucet"`
	Contracts        map[string]string `yaml:"contracts"`
}

type FaucetConfig struct {
	Enabled          bool   `yaml:"enabled"`
	PrivateKeyBase58 string `yaml:"privateKeyBase58"`
	FromAddress      string `yaml:"fromAddress"`
	ComputeUnitPrice uint64 `yaml:"computeUnitPrice"`
}

type Token struct {
	Code        string `yaml:"code"`
	NetworkCode string `yaml:"networkCode"`
	MintAddress string `yaml:"mintAddress"`
	Decimals    uint8  `yaml:"decimals"`
}

type ContractBundle struct {
	NetworkCode string            `yaml:"networkCode"`
	Addresses   map[string]string `yaml:"addresses"`
	ABI         string            `yaml:"abi"`
}
