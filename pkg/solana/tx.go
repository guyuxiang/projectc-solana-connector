package solana

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	SystemProgramAddress = "11111111111111111111111111111111"
)

var ErrInvalidPrivateKey = errors.New("invalid solana private key")

func BuildNativeTransferTx(privateKeyBase58 string, toAddress string, recentBlockhash string, lamports uint64) (string, string, error) {
	signer, fromAddress, err := signerFromPrivateKey(privateKeyBase58)
	if err != nil {
		return "", "", err
	}

	fromBytes, err := DecodeBase58(fromAddress)
	if err != nil || len(fromBytes) != ed25519.PublicKeySize {
		return "", "", fmt.Errorf("decode from address: %w", ErrBase58)
	}
	toBytes, err := DecodeBase58(toAddress)
	if err != nil || len(toBytes) != ed25519.PublicKeySize {
		return "", "", fmt.Errorf("decode accept address: %w", ErrBase58)
	}
	systemProgramBytes, _ := DecodeBase58(SystemProgramAddress)

	accountKeys := [][]byte{fromBytes, toBytes, systemProgramBytes}
	instructions := make([]compiledInstruction, 0, 1)

	transferData := make([]byte, 12)
	binary.LittleEndian.PutUint32(transferData[:4], 2)
	binary.LittleEndian.PutUint64(transferData[4:], lamports)
	instructions = append(instructions, compiledInstruction{
		ProgramIndex: 2,
		AccountIdxs:  []byte{0, 1},
		Data:         transferData,
	})

	blockhashBytes, err := DecodeBase58(recentBlockhash)
	if err != nil || len(blockhashBytes) != 32 {
		return "", "", fmt.Errorf("decode recent blockhash: %w", ErrBase58)
	}

	message := encodeMessage(message{
		NumRequiredSignatures: 1,
		NumReadonlySigned:     0,
		NumReadonlyUnsigned:   1,
		AccountKeys:           accountKeys,
		RecentBlockhash:       blockhashBytes,
		Instructions:          instructions,
	})

	signature := ed25519.Sign(signer, message)
	txBytes := encodeCompactU16(1)
	txBytes = append(txBytes, signature...)
	txBytes = append(txBytes, message...)
	return base64.StdEncoding.EncodeToString(txBytes), fromAddress, nil
}

func signerFromPrivateKey(privateKeyBase58 string) (ed25519.PrivateKey, string, error) {
	raw, err := DecodeBase58(privateKeyBase58)
	if err != nil {
		return nil, "", err
	}
	switch len(raw) {
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), EncodeBase58(raw[32:]), nil
	case ed25519.SeedSize:
		key := ed25519.NewKeyFromSeed(raw)
		return key, EncodeBase58(key[32:]), nil
	default:
		return nil, "", ErrInvalidPrivateKey
	}
}

type message struct {
	NumRequiredSignatures byte
	NumReadonlySigned     byte
	NumReadonlyUnsigned   byte
	AccountKeys           [][]byte
	RecentBlockhash       []byte
	Instructions          []compiledInstruction
}

type compiledInstruction struct {
	ProgramIndex byte
	AccountIdxs  []byte
	Data         []byte
}

func encodeMessage(msg message) []byte {
	out := []byte{msg.NumRequiredSignatures, msg.NumReadonlySigned, msg.NumReadonlyUnsigned}
	out = append(out, encodeCompactU16(len(msg.AccountKeys))...)
	for _, key := range msg.AccountKeys {
		out = append(out, key...)
	}
	out = append(out, msg.RecentBlockhash...)
	out = append(out, encodeCompactU16(len(msg.Instructions))...)
	for _, ix := range msg.Instructions {
		out = append(out, ix.ProgramIndex)
		out = append(out, encodeCompactU16(len(ix.AccountIdxs))...)
		out = append(out, ix.AccountIdxs...)
		out = append(out, encodeCompactU16(len(ix.Data))...)
		out = append(out, ix.Data...)
	}
	return out
}

func encodeCompactU16(v int) []byte {
	if v < 0 {
		return []byte{0}
	}
	out := make([]byte, 0, 3)
	value := uint32(v)
	for {
		elem := byte(value & 0x7f)
		value >>= 7
		if value == 0 {
			out = append(out, elem)
			return out
		}
		out = append(out, elem|0x80)
	}
}
