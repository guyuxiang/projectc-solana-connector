package controller

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	"github.com/guyuxiang/projectc-solana-connector/pkg/service"
)

type ChainController interface {
	TxSend(c *gin.Context)
	Faucet(c *gin.Context)
	TxQuery(c *gin.Context)
	AddressBalance(c *gin.Context)
	TokenSupply(c *gin.Context)
	TokenBalance(c *gin.Context)
	LatestBlock(c *gin.Context)
	TxSubscribe(c *gin.Context)
	AddressSubscribe(c *gin.Context)
	TxSubscribeCancel(c *gin.Context)
	AddressSubscribeCancel(c *gin.Context)
	BlockSync(c *gin.Context)
}

func NewChainController() ChainController {
	app := service.GetApp()
	return &chainController{
		chain:        app.Chain,
		subscription: app.Subscription,
	}
}

type chainController struct {
	chain        service.ChainService
	subscription service.SubscriptionService
}

func (cc *chainController) TxSend(c *gin.Context) {
	var req models.TxSendRequest
	if !bindJSON(c, &req) {
		return
	}
	txCode, err := cc.chain.SendSignedTransaction(c.Request.Context(), req.TxSignResult)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := cc.autoSubscribeTx(c, txCode); err != nil {
		fail(c, http.StatusInternalServerError, fmt.Errorf("tx sent successfully but auto subscribe failed, txCode=%s: %w", txCode, err))
		return
	}
	ok(c, models.TxSendResponse{TxCode: txCode})
}

func (cc *chainController) Faucet(c *gin.Context) {
	var req models.FaucetRequest
	if !bindJSON(c, &req) {
		return
	}
	txCode, err := cc.chain.Faucet(c.Request.Context(), req)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := cc.autoSubscribeTx(c, txCode); err != nil {
		fail(c, http.StatusInternalServerError, fmt.Errorf("faucet tx sent successfully but auto subscribe failed, txCode=%s: %w", txCode, err))
		return
	}
	ok(c, models.TxSendResponse{TxCode: txCode})
}

func (cc *chainController) TxQuery(c *gin.Context) {
	var req models.TxQueryRequest
	if !bindJSON(c, &req) {
		return
	}
	resp, err := cc.chain.QueryTransaction(c.Request.Context(), req.TxCode)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, resp)
}

func (cc *chainController) AddressBalance(c *gin.Context) {
	var req models.AddressBalanceRequest
	if !bindJSON(c, &req) {
		return
	}
	resp, err := cc.chain.GetAddressBalance(c.Request.Context(), req.Address)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, resp)
}

func (cc *chainController) TokenSupply(c *gin.Context) {
	var req models.TokenSupplyRequest
	if !bindJSON(c, &req) {
		return
	}
	resp, err := cc.chain.GetTokenSupply(c.Request.Context(), req.TokenCode)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, resp)
}

func (cc *chainController) TokenBalance(c *gin.Context) {
	var req models.TokenBalanceRequest
	if !bindJSON(c, &req) {
		return
	}
	resp, err := cc.chain.GetTokenBalance(c.Request.Context(), req.TokenCode, req.Address)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, resp)
}

func (cc *chainController) LatestBlock(c *gin.Context) {
	resp, err := cc.chain.GetLatestBlock(c.Request.Context())
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, resp)
}

func (cc *chainController) TxSubscribe(c *gin.Context) {
	var req models.TxSubscribeRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := cc.subscription.RegisterTxSubscription(req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, nil)
}

func (cc *chainController) AddressSubscribe(c *gin.Context) {
	var req models.AddressSubscribeRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := cc.subscription.RegisterAddressSubscription(req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, nil)
}

func (cc *chainController) TxSubscribeCancel(c *gin.Context) {
	var req models.TxSubscribeCancelRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := cc.subscription.CancelTxSubscription(req.TxCode); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, nil)
}

func (cc *chainController) AddressSubscribeCancel(c *gin.Context) {
	var req models.AddressSubscribeCancelRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := cc.subscription.CancelAddressSubscription(req.Address); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, nil)
}

func (cc *chainController) BlockSync(c *gin.Context) {
	var req models.BlockSyncRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := cc.subscription.SyncBlockRange(c.Request.Context(), req.BeginBlockNumber, req.EndBlockNumber); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, nil)
}

func bindJSON(c *gin.Context, req interface{}) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return false
	}
	return true
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, models.Response{
		Code:    "200",
		Message: "",
		Data:    data,
	})
}

func fail(c *gin.Context, status int, err error) {
	c.JSON(status, models.ErrorResponse{
		Code:    strconvStatus(status),
		Message: err.Error(),
	})
}

func strconvStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "400"
	case http.StatusNotFound:
		return "404"
	default:
		return "500"
	}
}

func (cc *chainController) autoSubscribeTx(c *gin.Context, txCode string) error {
	latest, err := cc.chain.GetLatestBlock(c.Request.Context())
	if err != nil {
		return err
	}
	endBlock := latest.BlockNumber + config.GetConfig().Connector.TxSubscribeWindow
	return cc.subscription.RegisterTxSubscription(models.TxSubscribeRequest{
		TxCode: txCode,
		SubscribeRange: models.SubscribeRange{
			EndBlockNumber: &endBlock,
		},
	})
}
