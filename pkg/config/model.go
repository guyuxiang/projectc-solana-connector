package config

type Config struct {
	Server    *Server           `yaml:"server"`
	Auth      *Auth             `yaml:"auth"`
	Mysql     *MySQLConfig      `yaml:"mysql"`
	Callback  *CallbackConfig   `yaml:"callback"`
	RabbitMQ  *RabbitMQConfig   `yaml:"rabbitmq"`
	Wallet    *WalletConfig     `yaml:"wallet"`
	Gin       *Gin              `yaml:"gin"`
	Log       *Log              `yaml:"log"`
	Connector *Connector        `yaml:"connector"`
	Networks  *SolanaNetwork    `yaml:"networks"`
	Tokens    map[string]*Token `yaml:"-" mapstructure:"-"`
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
	Pollintervalms    int    `yaml:"pollintervalms"`
	Txsubscribewindow uint64 `yaml:"txsubscribewindow"`
}

type CallbackConfig struct {
	Httpurl  string `yaml:"httpurl"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type RabbitMQConfig struct {
	Enabled          bool   `yaml:"enabled"`
	URL              string `yaml:"url"`
	TxExchange       string `yaml:"txExchange"`
	RollbackExchange string `yaml:"rollbackExchange"`
	RetryDelayMs     int    `yaml:"retryDelayMs"`
	MaxRetry         int    `yaml:"maxRetry"`
	PrefetchCount    int    `yaml:"prefetchCount"`
}

type MySQLConfig struct {
	Dsn            string `yaml:"dsn"`
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Database       string `yaml:"database"`
	Maxopenconns   int    `yaml:"maxopenconns"`
	Maxidleconns   int    `yaml:"maxidleconns"`
	Connmaxlifesec int    `yaml:"connmaxlifesec"`
}

type SolanaNetwork struct {
	Networkcode  string `yaml:"networkcode"`
	Chainid      string `yaml:"chainid"`
	Nativesymbol string `yaml:"nativesymbol"`
	Rpcurl       string `yaml:"rpcurl"`
	Wsurl        string `yaml:"wsurl"`
}

type WalletConfig struct {
	Privatekeybase58 string `yaml:"privatekeybase58"`
	Fromaddress      string `yaml:"fromaddress"`
}

type Token struct {
	Networkcode string `yaml:"networkcode"`
	Mintaddress string `yaml:"mintaddress"`
	Decimals    uint8  `yaml:"decimals"`
}
