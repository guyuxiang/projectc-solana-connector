package config

import (
	"fmt"
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
	applyEnvOverrides(&c, os.Environ())
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
	if c.Mysql == nil {
		c.Mysql = &MySQLConfig{}
	}
	if c.Callback == nil {
		c.Callback = &CallbackConfig{}
	}
	if c.Wallet == nil {
		c.Wallet = &WalletConfig{}
	}
	if c.Connector == nil {
		c.Connector = &Connector{}
	}
	if c.Connector.Pollintervalms == 0 {
		c.Connector.Pollintervalms = 15000
	}
	if c.Connector.Txsubscribewindow == 0 {
		c.Connector.Txsubscribewindow = 300
	}
	if c.Mysql.Maxopenconns == 0 {
		c.Mysql.Maxopenconns = 10
	}
	if c.Mysql.Maxidleconns == 0 {
		c.Mysql.Maxidleconns = 5
	}
	if c.Mysql.Connmaxlifesec == 0 {
		c.Mysql.Connmaxlifesec = 300
	}
	if c.Mysql.Dsn == "" && c.Mysql.Host != "" {
		c.Mysql.Dsn = buildMySQLDSN(c.Mysql)
	}
	if c.Networks == nil {
		c.Networks = &SolanaNetwork{}
	}
	if c.Tokens == nil {
		c.Tokens = make(map[string]*Token)
	}
	if c.Networks.Networkcode == "" {
		c.Networks.Networkcode = "solana"
	}
	if c.Networks.Nativesymbol == "" {
		c.Networks.Nativesymbol = "SOL"
	}
}

func applyEnvOverrides(c *Config, envs []string) {
	applyNetworkEndpointEnvOverrides(c, envs)
}

func applyNetworkEndpointEnvOverrides(c *Config, envs []string) {
	if c.Networks == nil {
		c.Networks = &SolanaNetwork{}
	}

	for _, env := range envs {
		key, value, ok := strings.Cut(env, "=")
		if !ok || value == "" {
			continue
		}

		field := parseNetworkEnvKey(key)
		if field == "" {
			continue
		}

		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		switch field {
		case "rpcUrl":
			c.Networks.Rpcurl = value
		case "wsUrl":
			c.Networks.Wsurl = value
		}
	}
}

func parseNetworkEnvKey(key string) string {
	switch {
	case key == "NETWORKS_WS_URL" || key == "NETWORKS_WSURL" || key == "NETWORKS_SOLANA_WS_URL" || key == "NETWORKS_SOLANA_WSURL" || key == "NETWORKS_WS_ENDPOINTS" || key == "NETWORKS_WSENDPOINTS" || key == "NETWORKS_SOLANA_WS_ENDPOINTS" || key == "NETWORKS_SOLANA_WSENDPOINTS":
		return "wsUrl"
	case key == "NETWORKS_RPC_URL" || key == "NETWORKS_RPCURL" || key == "NETWORKS_SOLANA_RPC_URL" || key == "NETWORKS_SOLANA_RPCURL" || key == "NETWORKS_ENDPOINTS" || key == "NETWORKS_SOLANA_ENDPOINTS":
		return "rpcUrl"
	default:
		return ""
	}
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
