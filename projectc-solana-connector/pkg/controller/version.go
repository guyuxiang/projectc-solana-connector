package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
)

func Version(c *gin.Context) {
	c.JSON(200, config.FLAG_KEY_SERVER_VERSION)
}
