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

// TxSend godoc
// @Summary Send a signed Solana transaction
// @Description Broadcast a signed transaction and automatically create a transaction subscription window.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.TxSendRequest true "Signed transaction payload"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /inner/chain-invoke/solana/common/tx-send [post]
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

// Faucet godoc
// @Summary Send faucet tokens
// @Description Send native SOL from the configured faucet account to the target address and automatically subscribe the transaction.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.FaucetRequest true "Faucet request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /inner/chain-invoke/solana/wallet/faucet [post]
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

// TxQuery godoc
// @Summary Query a transaction
// @Description Query on-chain transaction status, summary data, and decoded events by transaction code.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.TxQueryRequest true "Transaction query request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data/solana/common/tx-query [post]
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

// AddressBalance godoc
// @Summary Query native balance by address
// @Description Get the native SOL balance of a wallet address on the configured Solana network.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.AddressBalanceRequest true "Address balance request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data/solana/common/address-balance [post]
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

// TokenSupply godoc
// @Summary Query token supply
// @Description Get the total supply of a configured SPL token by token code.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.TokenSupplyRequest true "Token supply request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data/solana/common/token-supply [post]
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

// TokenBalance godoc
// @Summary Query token balance
// @Description Get the balance of a configured SPL token for a specific wallet address.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.TokenBalanceRequest true "Token balance request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data/solana/common/token-balance [post]
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

// LatestBlock godoc
// @Summary Query latest block
// @Description Get the latest observed Solana block number and timestamp.
// @Tags Solana
// @Accept json
// @Produce json
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data/solana/common/latest-block [post]
func (cc *chainController) LatestBlock(c *gin.Context) {
	resp, err := cc.chain.GetLatestBlock(c.Request.Context())
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ok(c, resp)
}

// TxSubscribe godoc
// @Summary Subscribe transaction updates
// @Description Register a transaction subscription to watch a transaction until the configured end block.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.TxSubscribeRequest true "Transaction subscription request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data-subscribe/solana/tx-subscribe [post]
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

// AddressSubscribe godoc
// @Summary Subscribe address activity
// @Description Register an address subscription to watch transactions related to a wallet address.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.AddressSubscribeRequest true "Address subscription request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data-subscribe/solana/address-subscribe [post]
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

// TxSubscribeCancel godoc
// @Summary Cancel transaction subscription
// @Description Cancel an existing transaction subscription by transaction code.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.TxSubscribeCancelRequest true "Transaction subscription cancel request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data-subscribe/solana/tx-subscribe-cancel [post]
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

// AddressSubscribeCancel godoc
// @Summary Cancel address subscription
// @Description Cancel an existing address subscription by wallet address.
// @Tags Solana
// @Accept json
// @Produce json
// @Param request body models.AddressSubscribeCancelRequest true "Address subscription cancel request"
// @Success 200 {object} models.Response
// @Failure 400 {object} models.ErrorResponse
// @Router /inner/chain-data-subscribe/solana/address-subscribe-cancel [post]
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
