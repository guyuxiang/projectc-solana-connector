package service

import (
	"encoding/json"
	"testing"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/solana"
)

func TestToChainTxAndEvents(t *testing.T) {
	blockTime := int64(1700000000)
	parsed, _ := json.Marshal(map[string]interface{}{
		"type": "transfer",
		"info": map[string]interface{}{
			"source":      "from-address",
			"destination": "to-address",
			"lamports":    1500000000,
		},
	})

	tx := solana.TransactionResult{
		Slot:      88,
		BlockTime: &blockTime,
		Meta: solana.TransactionMeta{
			Fee: 5000,
		},
		Transaction: solana.ParsedTransactionRaw{
			Signatures: []string{"sig-1"},
			Message: solana.ParsedMessageRaw{
				AccountKeys: []solana.ParsedAccountKey{
					{Pubkey: "from-address", Signer: true},
				},
				Instructions: []solana.ParsedInstruction{
					{Program: "system", Parsed: parsed},
				},
			},
		},
	}

	network := &config.SolanaNetwork{LamportsPerToken: 1_000_000_000}
	chainTx := toChainTx(network, tx)
	if chainTx.Code != "sig-1" || chainTx.From != "from-address" || chainTx.To != "to-address" || chainTx.Amount != "1.5" {
		t.Fatalf("unexpected chain tx: %+v", chainTx)
	}
	events := toChainEvents(&config.Config{}, network, tx)
	if len(events) != 0 {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestToChainTxFallbacksToSignerAndProgramIDWithoutNativeTransfer(t *testing.T) {
	blockTime := int64(1700000000)
	parsed, _ := json.Marshal(map[string]interface{}{
		"type": "initializeMint",
		"info": map[string]interface{}{
			"mint": "mint-address",
		},
	})

	tx := solana.TransactionResult{
		Slot:      99,
		BlockTime: &blockTime,
		Meta: solana.TransactionMeta{
			Fee: 5000,
		},
		Transaction: solana.ParsedTransactionRaw{
			Signatures: []string{"sig-2"},
			Message: solana.ParsedMessageRaw{
				AccountKeys: []solana.ParsedAccountKey{
					{Pubkey: "signer-address", Signer: true},
				},
				Instructions: []solana.ParsedInstruction{
					{Program: "spl-token", ProgramID: "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", Parsed: parsed},
				},
			},
		},
	}

	network := &config.SolanaNetwork{LamportsPerToken: 1_000_000_000}
	chainTx := toChainTx(network, tx)
	if chainTx.From != "signer-address" || chainTx.To != "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA" || chainTx.Amount != "0" {
		t.Fatalf("unexpected fallback chain tx: %+v", chainTx)
	}
}

func TestToChainEventsTransformsSPLTokenBusinessEvents(t *testing.T) {
	blockTime := int64(1700000000)
	mintToParsed, _ := json.Marshal(map[string]interface{}{
		"type": "mintTo",
		"info": map[string]interface{}{
			"mint":    "mint-address",
			"account": "recipient-token-account",
			"amount":  "1250",
		},
	})
	burnParsed, _ := json.Marshal(map[string]interface{}{
		"type": "burn",
		"info": map[string]interface{}{
			"mint":    "mint-address",
			"account": "owner-token-account",
			"owner":   "owner-address",
			"amount":  "500",
		},
	})
	transferParsed, _ := json.Marshal(map[string]interface{}{
		"type": "transferChecked",
		"info": map[string]interface{}{
			"mint":        "mint-address",
			"source":      "source-token-account",
			"destination": "destination-token-account",
			"tokenAmount": map[string]interface{}{
				"uiAmountString": "2.5",
			},
		},
	})

	tx := solana.TransactionResult{
		Slot:      101,
		BlockTime: &blockTime,
		Transaction: solana.ParsedTransactionRaw{
			Signatures: []string{"sig-spl"},
			Message: solana.ParsedMessageRaw{
				AccountKeys: []solana.ParsedAccountKey{
					{Pubkey: "recipient-token-account"},
					{Pubkey: "owner-token-account"},
					{Pubkey: "source-token-account"},
					{Pubkey: "destination-token-account"},
				},
				Instructions: []solana.ParsedInstruction{
					{Program: "spl-token", Parsed: mintToParsed},
					{Program: "spl-token", Parsed: burnParsed},
					{Program: "spl-token", Parsed: transferParsed},
				},
			},
		},
		Meta: solana.TransactionMeta{
			PostTokenBalances: []solana.TokenBalanceRecord{
				{AccountIndex: 0, Mint: "mint-address", Owner: "recipient-address"},
				{AccountIndex: 1, Mint: "mint-address", Owner: "owner-address"},
				{AccountIndex: 2, Mint: "mint-address", Owner: "from-address"},
				{AccountIndex: 3, Mint: "mint-address", Owner: "to-address"},
			},
		},
	}

	cfg := &config.Config{
		Tokens: map[string]*config.Token{
			"DTT_GLUSD": {
				Code:        "DTT_GLUSD",
				NetworkCode: "solana-devnet",
				MintAddress: "mint-address",
				Decimals:    2,
			},
		},
	}
	network := &config.SolanaNetwork{Code: "solana-devnet", LamportsPerToken: 1_000_000_000}
	events := toChainEvents(cfg, network, tx)
	if len(events) != 3 {
		t.Fatalf("unexpected events length: %+v", events)
	}
	if events[0].Type != "RT_MINT" {
		t.Fatalf("unexpected mint event type: %+v", events[0])
	}
	if events[1].Type != "RT_BURN" {
		t.Fatalf("unexpected burn event type: %+v", events[1])
	}
	if events[2].Type != "RT_TRANSFER" {
		t.Fatalf("unexpected transfer event type: %+v", events[2])
	}

	mintData := events[0].Data.(map[string]interface{})
	if mintData["tokenCode"] != "DTT_GLUSD" || mintData["recipient"] != "recipient-address" {
		t.Fatalf("unexpected mint data: %+v", mintData)
	}
	burnData := events[1].Data.(map[string]interface{})
	if burnData["owner"] != "owner-address" {
		t.Fatalf("unexpected burn data: %+v", burnData)
	}
	transferData := events[2].Data.(map[string]interface{})
	if transferData["from"] != "from-address" || transferData["to"] != "to-address" || transferData["amount"] != 2.5 {
		t.Fatalf("unexpected transfer data: %+v", transferData)
	}
}
