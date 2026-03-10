package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/solana"
)

const (
	splTokenProgramID     = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	splToken2022ProgramID = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
)

type DomainEvent struct {
	Type string
	Data map[string]interface{}
}

type Decoder interface {
	ProgramID() string
	Decode(tx *EnrichedTransaction) ([]DomainEvent, error)
}

type EnrichedTransaction struct {
	Config       *config.Config
	NetworkCode  string
	Signature    string
	Timestamp    int64
	Transaction  solana.TransactionResult
	TokenAccount map[string]tokenAccountContext
}

func newEnrichedTransaction(cfg *config.Config, networkCode string, tx solana.TransactionResult) *EnrichedTransaction {
	signature := ""
	if len(tx.Transaction.Signatures) > 0 {
		signature = tx.Transaction.Signatures[0]
	}
	timestamp := int64(0)
	if tx.BlockTime != nil {
		timestamp = *tx.BlockTime * 1000
	}
	return &EnrichedTransaction{
		Config:       cfg,
		NetworkCode:  networkCode,
		Signature:    signature,
		Timestamp:    timestamp,
		Transaction:  tx,
		TokenAccount: buildTokenAccountContext(tx),
	}
}

type decoderRegistry struct {
	decoders       map[string]Decoder
	programAliases map[string]Decoder
}

func newDecoderRegistry(decoders ...Decoder) *decoderRegistry {
	items := make(map[string]Decoder, len(decoders))
	aliases := make(map[string]Decoder)
	for _, decoder := range decoders {
		items[decoder.ProgramID()] = decoder
		switch decoder.ProgramID() {
		case splTokenProgramID:
			aliases["spl-token"] = decoder
		case splToken2022ProgramID:
			aliases["spl-token-2022"] = decoder
		}
	}
	return &decoderRegistry{decoders: items, programAliases: aliases}
}

func (r *decoderRegistry) get(programID string, program string) Decoder {
	if decoder, ok := r.decoders[programID]; ok {
		return decoder
	}
	return r.programAliases[program]
}

var defaultDecoderRegistry = newDecoderRegistry(
	newSPLTokenDecoder(splTokenProgramID),
	newSPLTokenDecoder(splToken2022ProgramID),
)

type splTokenDecoder struct {
	programID string
}

func newSPLTokenDecoder(programID string) Decoder {
	return &splTokenDecoder{programID: programID}
}

func (d *splTokenDecoder) ProgramID() string {
	return d.programID
}

func (d *splTokenDecoder) Decode(tx *EnrichedTransaction) ([]DomainEvent, error) {
	events := make([]DomainEvent, 0)
	for _, instruction := range tx.Transaction.Transaction.Message.Instructions {
		if !strings.EqualFold(instruction.ProgramID, d.programID) &&
			!(strings.EqualFold(instruction.Program, "spl-token") && d.programID == splTokenProgramID) &&
			!(strings.EqualFold(instruction.Program, "spl-token-2022") && d.programID == splToken2022ProgramID) {
			continue
		}
		if len(instruction.Parsed) == 0 {
			continue
		}
		var payload parsedInstructionPayload
		if err := jsonUnmarshalInstruction(instruction, &payload); err != nil {
			return nil, err
		}
		event, ok := d.decodeInstruction(tx, payload)
		if !ok {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

func (d *splTokenDecoder) decodeInstruction(tx *EnrichedTransaction, payload parsedInstructionPayload) (DomainEvent, bool) {
	switch payload.Type {
	case "mintTo", "mintToChecked":
		mint, _ := payload.Info["mint"].(string)
		tokenCode, ok := resolveTokenCode(tx.Config, tx.NetworkCode, mint)
		if !ok {
			return DomainEvent{}, false
		}
		account, _ := payload.Info["account"].(string)
		recipient := resolveTokenAccountOwner(tx.TokenAccount, account)
		if recipient == "" {
			recipient = account
		}
		return DomainEvent{
			Type: "RT_MINT",
			Data: map[string]interface{}{
				"bid":       readString(payload.Info, "bid"),
				"tokenCode": tokenCode,
				"recipient": recipient,
				"amount":    resolveTokenAmount(tx.Config, tx.NetworkCode, mint, payload.Info),
			},
		}, true
	case "burn", "burnChecked":
		mint, _ := payload.Info["mint"].(string)
		tokenCode, ok := resolveTokenCode(tx.Config, tx.NetworkCode, mint)
		if !ok {
			return DomainEvent{}, false
		}
		owner := readString(payload.Info, "owner")
		if owner == "" {
			owner = readString(payload.Info, "authority")
		}
		if owner == "" {
			account, _ := payload.Info["account"].(string)
			owner = resolveTokenAccountOwner(tx.TokenAccount, account)
			if owner == "" {
				owner = account
			}
		}
		return DomainEvent{
			Type: "RT_BURN",
			Data: map[string]interface{}{
				"bid":       readString(payload.Info, "bid"),
				"tokenCode": tokenCode,
				"owner":     owner,
				"amount":    resolveTokenAmount(tx.Config, tx.NetworkCode, mint, payload.Info),
			},
		}, true
	case "transfer", "transferChecked":
		mint := readString(payload.Info, "mint")
		source := readString(payload.Info, "source")
		destination := readString(payload.Info, "destination")
		if mint == "" {
			mint = resolveTokenAccountMint(tx.TokenAccount, source)
		}
		tokenCode, ok := resolveTokenCode(tx.Config, tx.NetworkCode, mint)
		if !ok {
			return DomainEvent{}, false
		}
		from := resolveTokenAccountOwner(tx.TokenAccount, source)
		if from == "" {
			from = source
		}
		to := resolveTokenAccountOwner(tx.TokenAccount, destination)
		if to == "" {
			to = destination
		}
		return DomainEvent{
			Type: "RT_TRANSFER",
			Data: map[string]interface{}{
				"tokenCode": tokenCode,
				"from":      from,
				"to":        to,
				"amount":    resolveTokenAmount(tx.Config, tx.NetworkCode, mint, payload.Info),
			},
		}, true
	default:
		return DomainEvent{}, false
	}
}

func jsonUnmarshalInstruction(instruction solana.ParsedInstruction, out interface{}) error {
	if len(instruction.Parsed) == 0 {
		return fmt.Errorf("instruction has empty parsed payload")
	}
	return json.Unmarshal(instruction.Parsed, out)
}

func resolveTokenCode(cfg *config.Config, networkCode string, mint string) (string, bool) {
	if mint == "" || cfg == nil {
		return "", false
	}
	for tokenCode, token := range cfg.Tokens {
		if token == nil {
			continue
		}
		if token.NetworkCode == networkCode && strings.EqualFold(token.MintAddress, mint) {
			return tokenCode, true
		}
	}
	return "", false
}

func resolveTokenAmount(cfg *config.Config, networkCode string, mint string, info map[string]interface{}) float64 {
	if tokenAmount, ok := info["tokenAmount"].(map[string]interface{}); ok {
		if uiAmountString, ok := tokenAmount["uiAmountString"].(string); ok && uiAmountString != "" {
			value, err := strconv.ParseFloat(uiAmountString, 64)
			if err == nil {
				return value
			}
		}
		if uiAmount, ok := tokenAmount["uiAmount"].(float64); ok {
			return uiAmount
		}
	}

	raw := readString(info, "amount")
	if raw == "" {
		return asFloat(info["amount"])
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	decimals := uint8(0)
	if cfg != nil {
		for _, token := range cfg.Tokens {
			if token == nil {
				continue
			}
			if token.NetworkCode == networkCode && strings.EqualFold(token.MintAddress, mint) {
				decimals = token.Decimals
				break
			}
		}
	}
	for i := uint8(0); i < decimals; i++ {
		value /= 10
	}
	return value
}
