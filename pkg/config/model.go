package config

type Config struct {
	Server    *Server                   `yaml:"server"`
	Auth      *Auth                     `yaml:"auth"`
	MySQL     *MySQLConfig              `yaml:"mysql"`
	MQ        *MQConfig                 `yaml:"mq"`
	Gin       *Gin                      `yaml:"gin"`
	Log       *Log                      `yaml:"log"`
	Connector *Connector                `yaml:"connector"`
	Networks  map[string]*SolanaNetwork `yaml:"networks"`
	Tokens    map[string]*Token         `yaml:"tokens"`
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
	PollIntervalMs    int    `yaml:"pollIntervalMs"`
	TxSubscribeWindow uint64 `yaml:"txSubscribeWindow"`
}

type MQConfig struct {
	Mode              string `yaml:"mode"`
	URL               string `yaml:"url"`
	Host              string `yaml:"host"`
	Port              int    `yaml:"port"`
	Username          string `yaml:"username"`
	Password          string `yaml:"password"`
	VirtualHost       string `yaml:"virtualHost"`
	VirtualHostLegacy string `yaml:"virtual-host"`
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
	Code             string        `yaml:"code"`
	ChainID          string        `yaml:"chainId"`
	NativeSymbol     string        `yaml:"nativeSymbol"`
	LamportsPerToken uint64        `yaml:"lamportsPerToken"`
	Endpoints        []string      `yaml:"endpoints"`
	WsEndpoints      []string      `yaml:"wsEndpoints"`
	Faucet           *FaucetConfig `yaml:"faucet"`
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
