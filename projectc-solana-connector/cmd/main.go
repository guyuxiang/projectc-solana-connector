package main

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/route"
	"github.com/guyuxiang/projectc-solana-connector/pkg/util"
)

func main() {
	util.SetupSigusr1Trap()
	r := gin.Default()
	m := config.GetString(config.FLAG_KEY_GIN_MODE)
	gin.SetMode(m)

	route.InstallRoutes(r)
	serverBindAddr := fmt.Sprintf("%s:%d", config.GetString(config.FLAG_KEY_SERVER_HOST), config.GetInt(config.FLAG_KEY_SERVER_PORT))
	log.Infof("Run server at %s", serverBindAddr)
	r.Run(serverBindAddr) // listen and serve
}
