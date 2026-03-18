package solana

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestBase58RoundTrip(t *testing.T) {
	input := []byte{0, 0, 1, 2, 3, 4, 5, 255}
	encoded := EncodeBase58(input)
	decoded, err := DecodeBase58(encoded)
	if err != nil {
		t.Fatalf("DecodeBase58 returned error: %v", err)
	}
	if !bytes.Equal(input, decoded) {
		t.Fatalf("round trip mismatch: input=%v decoded=%v", input, decoded)
	}
}

func TestBuildNativeTransferTx(t *testing.T) {
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	privateKeyBase58 := EncodeBase58(seed)
	toSeed := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	toAddress := EncodeBase58(ed25519.NewKeyFromSeed(toSeed)[32:])
	blockhash := EncodeBase58(bytes.Repeat([]byte{1}, 32))

	txEncoded, fromAddress, err := BuildNativeTransferTx(privateKeyBase58, toAddress, blockhash, 12345)
	if err != nil {
		t.Fatalf("BuildNativeTransferTx returned error: %v", err)
	}
	if fromAddress == "" {
		t.Fatal("expected fromAddress to be populated")
	}
	if _, err := base64.StdEncoding.DecodeString(txEncoded); err != nil {
		t.Fatalf("expected base64 transaction payload, got error: %v", err)
	}
}
