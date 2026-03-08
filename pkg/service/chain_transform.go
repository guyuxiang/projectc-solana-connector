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

func toChainTx(networkCode string, network *config.SolanaNetwork, tx solana.TransactionResult) models.ChainTx {
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
		NetworkCode: networkCode,
		BlockNumber: tx.Slot,
		Timestamp:   timestamp,
		Status:      status,
		From:        from,
		To:          to,
		Amount:      amount,
		Fee:         strconv.FormatFloat(fromLamports(tx.Meta.Fee, network.LamportsPerToken), 'f', -1, 64),
	}
}

func toChainEvents(networkCode string, tx solana.TransactionResult) []models.ChainEvent {
	signature := ""
	if len(tx.Transaction.Signatures) > 0 {
		signature = tx.Transaction.Signatures[0]
	}
	timestamp := int64(0)
	if tx.BlockTime != nil {
		timestamp = *tx.BlockTime * 1000
	}

	events := make([]models.ChainEvent, 0, len(tx.Transaction.Message.Instructions))
	for idx, instruction := range tx.Transaction.Message.Instructions {
		payload, eventType, ok := normalizeInstructionEvent(instruction)
		if !ok {
			continue
		}
		events = append(events, models.ChainEvent{
			Code:        fmt.Sprintf("%s#%d", signature, idx),
			NetworkCode: networkCode,
			BlockNumber: tx.Slot,
			Timestamp:   timestamp,
			Type:        eventType,
			Data:        payload,
		})
	}
	return events
}

func normalizeInstructionEvent(instruction solana.ParsedInstruction) (map[string]interface{}, string, bool) {
	if len(instruction.Parsed) == 0 {
		return nil, "", false
	}

	var payload parsedInstructionPayload
	if err := json.Unmarshal(instruction.Parsed, &payload); err != nil {
		return nil, "", false
	}
	if payload.Type == "" {
		return nil, "", false
	}

	eventType := strings.ToUpper(instruction.Program) + "_" + strings.ToUpper(payload.Type)
	return map[string]interface{}{
		"program": instruction.Program,
		"type":    payload.Type,
		"info":    payload.Info,
	}, eventType, true
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
		return from, to, strconv.FormatFloat(fromLamports(uint64(asFloat(rawLamports)), network.LamportsPerToken), 'f', -1, 64), true
	}
	return "", "", "", false
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
