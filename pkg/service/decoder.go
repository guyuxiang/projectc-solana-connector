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
	Decode(cfg *config.Config, network *config.SolanaNetwork, tx solana.TransactionResult, instruction solana.ParsedInstruction) (DomainEvent, error)
}

type decoderRegistry struct {
	programAliases map[string]Decoder
}

type decoderRegistration struct {
	alias   string
	decoder Decoder
}

func newDecoderRegistry(registrations ...decoderRegistration) *decoderRegistry {
	aliases := make(map[string]Decoder)
	for _, registration := range registrations {
		if registration.alias != "" {
			aliases[registration.alias] = registration.decoder
		}
	}
	return &decoderRegistry{programAliases: aliases}
}

func (r *decoderRegistry) get(program string) Decoder {
	if decoder, ok := r.programAliases[program]; ok {
		return decoder
	}
	return nil
}

var defaultDecoderRegistry = newDecoderRegistry(
	decoderRegistration{
		alias:   "spl-token",
		decoder: newSPLTokenDecoder(),
	},
	decoderRegistration{
		alias:   "spl-token-2022",
		decoder: newSPLTokenDecoder(),
	},
)

type splTokenDecoder struct{}

func newSPLTokenDecoder() Decoder {
	return &splTokenDecoder{}
}

func (d *splTokenDecoder) Decode(cfg *config.Config, network *config.SolanaNetwork, tx solana.TransactionResult, instruction solana.ParsedInstruction) (DomainEvent, error) {
	if len(instruction.Parsed) == 0 {
		return DomainEvent{}, nil
	}

	var payload parsedInstructionPayload
	if err := jsonUnmarshalInstruction(instruction, &payload); err != nil {
		return DomainEvent{}, err
	}
	event, ok := d.decodeInstruction(cfg, network, tx, payload)
	if !ok {
		return DomainEvent{}, nil
	}
	return event, nil
}

func (d *splTokenDecoder) decodeInstruction(cfg *config.Config, network *config.SolanaNetwork, tx solana.TransactionResult, payload parsedInstructionPayload) (DomainEvent, bool) {
	switch payload.Type {
	case "mintTo", "mintToChecked":
		mint, _ := payload.Info["mint"].(string)
		tokenCode, ok := resolveTokenCode(cfg, network, mint)
		if !ok {
			return DomainEvent{}, false
		}
		account, _ := payload.Info["account"].(string)
		recipient := resolveTokenAccountOwner(tx, account)
		if recipient == "" {
			recipient = account
		}
		return DomainEvent{
			Type: "RT_MINT",
			Data: map[string]interface{}{
				"bid":       readString(payload.Info, "bid"),
				"tokenCode": tokenCode,
				"recipient": recipient,
				"amount":    resolveTokenAmount(cfg, network, mint, payload.Info),
			},
		}, true
	case "burn", "burnChecked":
		mint, _ := payload.Info["mint"].(string)
		tokenCode, ok := resolveTokenCode(cfg, network, mint)
		if !ok {
			return DomainEvent{}, false
		}
		owner := readString(payload.Info, "owner")
		if owner == "" {
			owner = readString(payload.Info, "authority")
		}
		if owner == "" {
			account, _ := payload.Info["account"].(string)
			owner = resolveTokenAccountOwner(tx, account)
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
				"amount":    resolveTokenAmount(cfg, network, mint, payload.Info),
			},
		}, true
	case "transfer", "transferChecked":
		mint := readString(payload.Info, "mint")
		source := readString(payload.Info, "source")
		destination := readString(payload.Info, "destination")
		if mint == "" {
			mint = resolveTokenAccountMint(tx, source)
		}
		tokenCode, ok := resolveTokenCode(cfg, network, mint)
		if !ok {
			return DomainEvent{}, false
		}
		from := resolveTokenAccountOwner(tx, source)
		if from == "" {
			from = source
		}
		to := resolveTokenAccountOwner(tx, destination)
		if to == "" {
			to = destination
		}
		return DomainEvent{
			Type: "RT_TRANSFER",
			Data: map[string]interface{}{
				"tokenCode": tokenCode,
				"from":      from,
				"to":        to,
				"amount":    resolveTokenAmount(cfg, network, mint, payload.Info),
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

func resolveTokenCode(cfg *config.Config, network *config.SolanaNetwork, mint string) (string, bool) {
	if mint == "" || cfg == nil {
		return "", false
	}
	for tokenCode, token := range cfg.Tokens {
		if token == nil {
			continue
		}
		if network != nil && token.NetworkCode == network.Code && strings.EqualFold(token.MintAddress, mint) {
			return tokenCode, true
		}
	}
	return "", false
}

func resolveTokenAmount(cfg *config.Config, network *config.SolanaNetwork, mint string, info map[string]interface{}) float64 {
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
			if network != nil && token.NetworkCode == network.Code && strings.EqualFold(token.MintAddress, mint) {
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
