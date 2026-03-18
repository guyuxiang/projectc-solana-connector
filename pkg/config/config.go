package config

import (
	"encoding/json"
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
	if c.MySQL == nil {
		c.MySQL = &MySQLConfig{}
	}
	if c.Callback == nil {
		c.Callback = &CallbackConfig{}
	}
	if c.Connector == nil {
		c.Connector = &Connector{}
	}
	if c.Connector.PollIntervalMs == 0 {
		c.Connector.PollIntervalMs = 15000
	}
	if c.Connector.TxSubscribeWindow == 0 {
		c.Connector.TxSubscribeWindow = 300
	}
	if c.MySQL.MaxOpenConns == 0 {
		c.MySQL.MaxOpenConns = c.MySQL.MaxOpenConnsV2
	}
	if c.MySQL.MaxOpenConns == 0 {
		c.MySQL.MaxOpenConns = 10
	}
	if c.MySQL.MaxIdleConns == 0 {
		c.MySQL.MaxIdleConns = 5
	}
	if c.MySQL.ConnMaxLifeSec == 0 {
		c.MySQL.ConnMaxLifeSec = 300
	}
	if c.MySQL.DSN == "" && c.MySQL.Host != "" {
		c.MySQL.DSN = buildMySQLDSN(c.MySQL)
	}
	if c.Networks == nil {
		c.Networks = make(map[string]*SolanaNetwork)
	}
	if c.Tokens == nil {
		c.Tokens = make(map[string]*Token)
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
	}
}

func applyEnvOverrides(c *Config, envs []string) {
	applyNetworkEndpointEnvOverrides(c, envs)
}

func applyNetworkEndpointEnvOverrides(c *Config, envs []string) {
	if c.Networks == nil {
		c.Networks = make(map[string]*SolanaNetwork)
	}

	for _, env := range envs {
		key, value, ok := strings.Cut(env, "=")
		if !ok || value == "" || !strings.HasPrefix(key, "NETWORKS_") {
			continue
		}

		networkCode, field := parseNetworkEnvKey(key)
		if networkCode == "" || field == "" {
			continue
		}

		endpoints := parseStringListEnv(value)
		if len(endpoints) == 0 {
			continue
		}

		network := c.Networks[networkCode]
		if network == nil {
			network = &SolanaNetwork{}
			c.Networks[networkCode] = network
		}

		switch field {
		case "endpoints":
			network.Endpoints = endpoints
		case "wsEndpoints":
			network.WsEndpoints = endpoints
		}
	}
}

func parseNetworkEnvKey(key string) (string, string) {
	const prefix = "NETWORKS_"
	trimmed := strings.TrimPrefix(key, prefix)

	switch {
	case strings.HasSuffix(trimmed, "_WS_ENDPOINTS"):
		return strings.ToLower(strings.TrimSuffix(trimmed, "_WS_ENDPOINTS")), "wsEndpoints"
	case strings.HasSuffix(trimmed, "_WSENDPOINTS"):
		return strings.ToLower(strings.TrimSuffix(trimmed, "_WSENDPOINTS")), "wsEndpoints"
	case strings.HasSuffix(trimmed, "_ENDPOINTS"):
		return strings.ToLower(strings.TrimSuffix(trimmed, "_ENDPOINTS")), "endpoints"
	default:
		return "", ""
	}
}

func parseStringListEnv(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var items []string
		if err := json.Unmarshal([]byte(trimmed), &items); err == nil {
			return cleanStringList(items)
		}
	}

	return cleanStringList(strings.Split(trimmed, ","))
}

func cleanStringList(items []string) []string {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		cleaned = append(cleaned, item)
	}
	return cleaned
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
