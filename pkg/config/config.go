package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type flagOpt struct {
	optName         string
	optDefaultValue interface{}
	optUsage        string
}

var c Config

func init() {

	// flag priority: cli > envvars > config > defaults
	for _, opt := range flagsOpts {
		viper.SetDefault(opt.optName, opt.optDefaultValue)
	}

	viper.SetConfigType("yaml")
	viper.SetConfigName(SERVICE_NAME)
	viper.AddConfigPath(fmt.Sprintf("/etc/%s/", SERVICE_NAME))   // path to look for the config file in
	viper.AddConfigPath(fmt.Sprintf("$HOME/.%s/", SERVICE_NAME)) // call multiple times to add many search paths
	viper.AddConfigPath("./etc/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		fmt.Fprintf(os.Stderr, "Fatal error config file: %s \n", err)
	}

	viper.SetEnvPrefix(SERVICE_NAME)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	for _, opt := range flagsOpts {
		switch opt.optDefaultValue.(type) {
		case int:
			pflag.Int(opt.optName, opt.optDefaultValue.(int), opt.optUsage)
		case string:
			pflag.String(opt.optName, opt.optDefaultValue.(string), opt.optUsage)
		case bool:
			pflag.Bool(opt.optName, opt.optDefaultValue.(bool), opt.optUsage)
		case []interface{}:
			continue
		default:
			continue
		}
	}

	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	err = viper.Unmarshal(&c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to decode into struct, %v\n", err)
	}
	normalizeConfig(&c)
}

func GetString(key string) string {
	return viper.GetString(key)
}

func GetInt(key string) int {
	return viper.GetInt(key)
}

func GetBool(key string) bool {
	return viper.GetBool(key)
}

func GetConfig() *Config {
	return &c
}

func normalizeConfig(c *Config) {
	if c.Server == nil {
		c.Server = &Server{}
	}
	if c.Auth == nil {
		c.Auth = &Auth{}
	}
	if c.Gin == nil {
		c.Gin = &Gin{}
	}
	if c.Log == nil {
		c.Log = &Log{}
	}
	if c.Connector == nil {
		c.Connector = &Connector{}
	}
	if c.Connector.RequestTimeoutMs == 0 {
		c.Connector.RequestTimeoutMs = 5000
	}
	if c.Connector.RetryTimes == 0 {
		c.Connector.RetryTimes = 2
	}
	if c.Connector.RetryBackoffMs == 0 {
		c.Connector.RetryBackoffMs = 300
	}
	if c.Connector.Commitment == "" {
		c.Connector.Commitment = "confirmed"
	}
	if c.Connector.PollIntervalMs == 0 {
		c.Connector.PollIntervalMs = 5000
	}
	if c.Connector.ReorgDepth == 0 {
		c.Connector.ReorgDepth = 32
	}
	if c.Connector.TxSubscribeWindow == 0 {
		c.Connector.TxSubscribeWindow = 300
	}
	if c.Connector.IdempotencyTtlSec == 0 {
		c.Connector.IdempotencyTtlSec = 3600
	}
	if c.Connector.SubscriptionBuffer == 0 {
		c.Connector.SubscriptionBuffer = 1024
	}
	if c.Connector.Callback == nil {
		c.Connector.Callback = &CallbackConfig{}
	}
	if c.Connector.SubscriptionStore == nil {
		c.Connector.SubscriptionStore = &SubscriptionStoreConfig{}
	}
	if c.Connector.Callback.Mode == "" {
		c.Connector.Callback.Mode = "log"
	}
	if c.Connector.Callback.Exchange == "" {
		c.Connector.Callback.Exchange = "tx_callback_fanout_exchange"
	}
	if c.Connector.Callback.ExchangeType == "" {
		c.Connector.Callback.ExchangeType = "fanout"
	}
	if c.Connector.Callback.ReconnectIntervalMs == 0 {
		c.Connector.Callback.ReconnectIntervalMs = 3000
	}
	if c.Connector.Callback.VirtualHost == "" && c.Connector.Callback.VirtualHostLegacy != "" {
		c.Connector.Callback.VirtualHost = c.Connector.Callback.VirtualHostLegacy
	}
	if c.Connector.Callback.URL == "" && c.Connector.Callback.Host != "" {
		c.Connector.Callback.URL = buildAMQPURL(c.Connector.Callback)
	}
	if c.Connector.SubscriptionStore.MySQL == nil {
		c.Connector.SubscriptionStore.MySQL = &MySQLConfig{}
	}
	if c.Connector.SubscriptionStore.MySQL.MaxOpenConns == 0 {
		c.Connector.SubscriptionStore.MySQL.MaxOpenConns = c.Connector.SubscriptionStore.MySQL.MaxOpenConnsV2
	}
	if c.Connector.SubscriptionStore.MySQL.MaxOpenConns == 0 {
		c.Connector.SubscriptionStore.MySQL.MaxOpenConns = 10
	}
	if c.Connector.SubscriptionStore.MySQL.MaxIdleConns == 0 {
		c.Connector.SubscriptionStore.MySQL.MaxIdleConns = 5
	}
	if c.Connector.SubscriptionStore.MySQL.ConnMaxLifeSec == 0 {
		c.Connector.SubscriptionStore.MySQL.ConnMaxLifeSec = 300
	}
	if c.Connector.SubscriptionStore.MySQL.DSN == "" && c.Connector.SubscriptionStore.MySQL.Host != "" {
		c.Connector.SubscriptionStore.MySQL.DSN = buildMySQLDSN(c.Connector.SubscriptionStore.MySQL)
	}
	if c.Networks == nil {
		c.Networks = make(map[string]*SolanaNetwork)
	}
	if c.Tokens == nil {
		c.Tokens = make(map[string]*Token)
	}
	if c.ContractInfo == nil {
		c.ContractInfo = make(map[string]*ContractBundle)
	}
	for code, network := range c.Networks {
		if network == nil {
			continue
		}
		if network.Code == "" {
			network.Code = code
		}
		if network.NativeSymbol == "" {
			network.NativeSymbol = "SOL"
		}
		if network.LamportsPerToken == 0 {
			network.LamportsPerToken = 1_000_000_000
		}
		if network.Contracts == nil {
			network.Contracts = make(map[string]string)
		}
	}
}

func buildAMQPURL(cfg *CallbackConfig) string {
	host := cfg.Host
	port := cfg.Port
	if port == 0 {
		port = 5672
	}
	vhost := cfg.VirtualHost
	if vhost == "" {
		vhost = "/"
	}
	return fmt.Sprintf(
		"amqp://%s:%s@%s:%d%s",
		url.QueryEscape(cfg.Username),
		url.QueryEscape(cfg.Password),
		host,
		port,
		encodeRabbitMQVHost(vhost),
	)
}

func encodeRabbitMQVHost(vhost string) string {
	if vhost == "" || vhost == "/" {
		return "/"
	}
	trimmed := strings.TrimPrefix(vhost, "/")
	return "/" + url.PathEscape(trimmed)
}

func buildMySQLDSN(cfg *MySQLConfig) string {
	host := cfg.Host
	port := cfg.Port
	if port == 0 {
		port = 3306
	}
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.Username,
		cfg.Password,
		host,
		port,
		cfg.Database,
	)
}
