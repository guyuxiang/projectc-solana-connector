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
	chainTx := toChainTx("solana-devnet", network, tx)
	if chainTx.Code != "sig-1" || chainTx.From != "from-address" || chainTx.To != "to-address" || chainTx.Amount != "1.5" {
		t.Fatalf("unexpected chain tx: %+v", chainTx)
	}
	events := toChainEvents("solana-devnet", tx)
	if len(events) != 1 || events[0].Type != "SYSTEM_TRANSFER" {
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
	chainTx := toChainTx("solana-devnet", network, tx)
	if chainTx.From != "signer-address" || chainTx.To != "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA" || chainTx.Amount != "0" {
		t.Fatalf("unexpected fallback chain tx: %+v", chainTx)
	}
}
