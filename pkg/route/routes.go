package route

import (
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/guyuxiang/projectc-solana-connector/docs"
	"github.com/guyuxiang/projectc-solana-connector/pkg/controller"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/gin-swagger/swaggerFiles"
)

// @title Swagger projectc-solana-connector
// @version 0.1.0
// @description This is a projectc-solana-connector.
// @contact.name guyuxiang
// @contact.url https://guyuxiang.github.io
// @contact.email guyuxiang@qq.com
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @BasePath /api/v1
func InstallRoutes(r *gin.Engine) {
	// Recovery middleware recovers from any panics and writes a 500 if there was one.
	r.Use(gin.Recovery())

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// a ping api test
	r.GET("/ping", controller.Ping)

	// get projectc-solana-connector version
	r.GET("/version", controller.Version)

	// config reload
	r.Any("/-/reload", func(c *gin.Context) {
		log.Info("===== Server Stop! Cause: Config Reload. =====")
		os.Exit(1)
	})

	chainController := controller.NewChainController()
	rootGroup := r.Group("/api/v1")
	rootGroup.GET("/ping", controller.Ping)

	registerChainRoutes(r, chainController)
	registerChainRoutes(rootGroup, chainController)
}

func registerChainRoutes(router gin.IRoutes, chainController controller.ChainController) {
	router.POST("/inner/chain-invoke/solana/common/tx-send", chainController.TxSend)
	router.POST("/inner/chain-invoke/solana/wallet/faucet", chainController.Faucet)
	router.POST("/inner/chain-data/solana/common/tx-query", chainController.TxQuery)
	router.POST("/inner/chain-data/solana/common/address-balance", chainController.AddressBalance)
	router.POST("/inner/chain-data/solana/common/token-supply", chainController.TokenSupply)
	router.POST("/inner/chain-data/solana/common/token-balance", chainController.TokenBalance)
	router.POST("/inner/chain-data/solana/common/latest-block", chainController.LatestBlock)
	router.POST("/inner/chain-data-subscribe/solana/tx-subscribe", chainController.TxSubscribe)
	router.POST("/inner/chain-data-subscribe/solana/address-subscribe", chainController.AddressSubscribe)
	router.POST("/inner/chain-data-subscribe/solana/tx-subscribe-cancel", chainController.TxSubscribeCancel)
	router.POST("/inner/chain-data-subscribe/solana/address-subscribe-cancel", chainController.AddressSubscribeCancel)
}
