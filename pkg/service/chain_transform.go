package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	"github.com/guyuxiang/projectc-solana-connector/pkg/solana"
)

type parsedInstructionPayload struct {
	Type string                 `json:"type"`
	Info map[string]interface{} `json:"info"`
}

func toChainTx(network *config.SolanaNetwork, tx solana.TransactionResult) models.ChainTx {
	signature := ""
	if len(tx.Transaction.Signatures) > 0 {
		signature = tx.Transaction.Signatures[0]
	}
	from, to, amount := extractChainTxParties(tx, network)
	timestamp := int64(0)
	if tx.BlockTime != nil {
		timestamp = *tx.BlockTime * 1000
	}
	status := "SUCCESS"
	if tx.Meta.Err != nil {
		status = "FAILED"
	}

	return models.ChainTx{
		Code:        signature,
		NetworkCode: network.Networkcode,
		BlockNumber: tx.Slot,
		Timestamp:   timestamp,
		Status:      status,
		From:        from,
		To:          to,
		Amount:      amount,
		Fee:         strconv.FormatFloat(fromLamports(tx.Meta.Fee, solanaLamportsPerSOL), 'f', -1, 64),
	}
}

func toChainEvents(cfg *config.Config, network *config.SolanaNetwork, tx solana.TransactionResult) []models.ChainEvent {
	events := make([]models.ChainEvent, 0, len(tx.Transaction.Message.Instructions))
	eventIdx := 0
	signature := ""
	if len(tx.Transaction.Signatures) > 0 {
		signature = tx.Transaction.Signatures[0]
	}
	timestamp := int64(0)
	if tx.BlockTime != nil {
		timestamp = *tx.BlockTime * 1000
	}

	for _, instruction := range tx.Transaction.Message.Instructions {
		if decoder := defaultDecoderRegistry.get(instruction.Program); decoder != nil {
			domainEvent, err := decoder.Decode(cfg, network, tx, instruction)
			if err == nil && domainEvent.Type != "" {
				events = append(events, models.ChainEvent{
					Code:        fmt.Sprintf("%s#%d", signature, eventIdx),
					NetworkCode: network.Networkcode,
					BlockNumber: tx.Slot,
					Timestamp:   timestamp,
					Type:        domainEvent.Type,
					Data:        domainEvent.Data,
				})
				eventIdx++
			}
			continue
		}
	}
	return events
}

func extractChainTxParties(tx solana.TransactionResult, network *config.SolanaNetwork) (string, string, string) {
	if from, to, amount, ok := extractNativeTransferSummary(tx, network); ok {
		return from, to, amount
	}

	from := ""
	for _, key := range tx.Transaction.Message.AccountKeys {
		if key.Signer {
			from = key.Pubkey
			break
		}
	}

	to := ""
	if len(tx.Transaction.Message.Instructions) > 0 {
		to = tx.Transaction.Message.Instructions[0].ProgramID
	}
	return from, to, "0"
}

func extractNativeTransferSummary(tx solana.TransactionResult, network *config.SolanaNetwork) (string, string, string, bool) {
	for _, instruction := range tx.Transaction.Message.Instructions {
		if instruction.Program != "system" || len(instruction.Parsed) == 0 {
			continue
		}

		var payload parsedInstructionPayload
		if err := json.Unmarshal(instruction.Parsed, &payload); err != nil {
			continue
		}
		if payload.Type != "transfer" {
			continue
		}

		from, _ := payload.Info["source"].(string)
		to, _ := payload.Info["destination"].(string)
		rawLamports, ok := payload.Info["lamports"]
		if !ok {
			return from, to, "0", true
		}
		return from, to, strconv.FormatFloat(fromLamports(uint64(asFloat(rawLamports)), solanaLamportsPerSOL), 'f', -1, 64), true
	}
	return "", "", "", false
}

type tokenAccountContext struct {
	Mint  string
	Owner string
}

func resolveTokenAccountContext(tx solana.TransactionResult, account string) (tokenAccountContext, bool) {
	if account == "" {
		return tokenAccountContext{}, false
	}
	for _, record := range tx.Meta.PostTokenBalances {
		if ctx, ok := resolveTokenAccountContextFromRecord(tx, account, record); ok {
			return ctx, true
		}
	}
	for _, record := range tx.Meta.PreTokenBalances {
		if ctx, ok := resolveTokenAccountContextFromRecord(tx, account, record); ok {
			return ctx, true
		}
	}
	return tokenAccountContext{}, false
}

func resolveTokenAccountContextFromRecord(tx solana.TransactionResult, account string, record solana.TokenBalanceRecord) (tokenAccountContext, bool) {
	if int(record.AccountIndex) >= len(tx.Transaction.Message.AccountKeys) {
		return tokenAccountContext{}, false
	}
	pubkey := tx.Transaction.Message.AccountKeys[record.AccountIndex].Pubkey
	if pubkey == "" || !strings.EqualFold(pubkey, account) {
		return tokenAccountContext{}, false
	}
	return tokenAccountContext{
		Mint:  record.Mint,
		Owner: record.Owner,
	}, true
}

func resolveTokenAccountOwner(tx solana.TransactionResult, account string) string {
	if item, ok := resolveTokenAccountContext(tx, account); ok {
		return item.Owner
	}
	return ""
}

func resolveTokenAccountMint(tx solana.TransactionResult, account string) string {
	if item, ok := resolveTokenAccountContext(tx, account); ok {
		return item.Mint
	}
	return ""
}

func readString(m map[string]interface{}, key string) string {
	if value, ok := m[key].(string); ok {
		return value
	}
	return ""
}

func asFloat(v interface{}) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case int64:
		return float64(value)
	case int:
		return float64(value)
	case json.Number:
		f, _ := value.Float64()
		return f
	default:
		return 0
	}
}
