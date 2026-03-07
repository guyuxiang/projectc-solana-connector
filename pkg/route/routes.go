package route

import (
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/guyuxiang/projectc-solana-connector/docs"
	"github.com/guyuxiang/projectc-solana-connector/pkg/controller"
	"github.com/guyuxiang/projectc-solana-connector/pkg/log"
	"github.com/guyuxiang/projectc-solana-connector/pkg/middleware"
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

	rootGroup := r.Group("/api/v1")
	// AuthRequired middleware that provide basic auth
	rootGroup.Use(middleware.BasicAuthMiddleware())

	{
		// a ping api to test basic auth
		rootGroup.GET("/ping", controller.Ping)
	}

	{
		toDoController := controller.NewToDoController()
		rootGroup.GET("/todo/get", toDoController.GetToDo)
	}
}
