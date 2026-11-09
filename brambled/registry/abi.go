package registry

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

// Keccak256 hashes data with Ethereum's Keccak-256.
func Keccak256(data ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, d := range data {
		h.Write(d)
	}
	return h.Sum(nil)
}

func topic(signature string) string { return "0x" + hexEncode(Keccak256([]byte(signature))) }

// Event topics of the ENSv2 registry and PermissionedResolver (contracts-v2 07e55a0).
var (
	topicLabelRegistered    = topic("LabelRegistered(uint256,bytes32,string,address,uint64,address)")
	topicLabelUnregistered  = topic("LabelUnregistered(uint256,address)")
	topicExpiryUpdated      = topic("ExpiryUpdated(uint256,uint64,address)")
	topicResolverUpdated    = topic("ResolverUpdated(uint256,address,address)")
	topicSubregistryUpdated = topic("SubregistryUpdated(uint256,address,address)")
	topicTokenRegenerated   = topic("TokenRegenerated(uint256,uint256)")
	topicTextUpdated        = topic("TextUpdated(uint256,string,string,string)")
	topicLinked             = topic("Linked(uint256,bytes32,bytes)")
	topicCleared            = topic("Cleared(uint256)")
)

func hexEncode(b []byte) string { return hex.EncodeToString(b) }

func hexDecode(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}

// NormalizeAddress returns a lowercase 0x-prefixed address, or an error.
func NormalizeAddress(a string) (string, error) {
	b, err := hexDecode(a)
	if err != nil || len(b) != 20 {
		return "", fmt.Errorf("invalid address %q", a)
	}
	return "0x" + hexEncode(b), nil
}

// topicAddress reads an address from an indexed topic.
func topicAddress(t string) string {
	b, _ := hexDecode(t)
	if len(b) < 20 {
		return ""
	}
	return "0x" + hexEncode(b[len(b)-20:])
}

// topicUint reads a uint256 from an indexed topic as a decimal string.
func topicUint(t string) string {
	b, _ := hexDecode(t)
	return new(big.Int).SetBytes(b).String()
}

// abiWords splits ABI-encoded data into 32-byte words.
type abiData []byte

func (d abiData) word(i int) ([]byte, error) {
	if len(d) < (i+1)*32 {
		return nil, errors.New("abi data too short")
	}
	return d[i*32 : (i+1)*32], nil
}

func (d abiData) uint64At(i int) (uint64, error) {
	w, err := d.word(i)
	if err != nil {
		return 0, err
	}
	n := new(big.Int).SetBytes(w)
	if !n.IsUint64() {
		return 0, errors.New("value exceeds uint64")
	}
	return n.Uint64(), nil
}

func (d abiData) addressAt(i int) (string, error) {
	w, err := d.word(i)
	if err != nil {
		return "", err
	}
	return "0x" + hexEncode(w[12:]), nil
}

// bytesAt reads a dynamic bytes or string value whose offset is in word i.
func (d abiData) bytesAt(i int) ([]byte, error) {
	off, err := d.uint64At(i)
	if err != nil {
		return nil, err
	}
	if off+32 > uint64(len(d)) {
		return nil, errors.New("abi offset out of range")
	}
	n := new(big.Int).SetBytes(d[off : off+32])
	if !n.IsUint64() || off+32+n.Uint64() > uint64(len(d)) {
		return nil, errors.New("abi length out of range")
	}
	return d[off+32 : off+32+n.Uint64()], nil
}

// encodeStringCall ABI-encodes `selector(string)` with one string argument.
func encodeStringCall(signature, arg string) []byte {
	out := append([]byte(nil), Keccak256([]byte(signature))[:4]...)
	out = append(out, leftPad(big.NewInt(32).Bytes())...)
	out = append(out, leftPad(big.NewInt(int64(len(arg))).Bytes())...)
	padded := make([]byte, (len(arg)+31)/32*32)
	copy(padded, arg)
	return append(out, padded...)
}

func leftPad(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// Namehash computes the ENS namehash of a dot-separated, already normalized name.
func Namehash(name string) []byte {
	node := make([]byte, 32)
	if name == "" {
		return node
	}
	labels := strings.Split(name, ".")
	for i := len(labels) - 1; i >= 0; i-- {
		node = Keccak256(node, Keccak256([]byte(labels[i])))
	}
	return node
}

// DNSEncode returns the DNS wire format of a dot-separated name, as ENS resolvers expect.
func DNSEncode(name string) ([]byte, error) {
	var out []byte
	for _, l := range strings.Split(name, ".") {
		if l == "" || len(l) > 63 {
			return nil, fmt.Errorf("invalid label in %q", name)
		}
		out = append(out, byte(len(l)))
		out = append(out, l...)
	}
	return append(out, 0), nil
}

// LabelHash returns keccak256(label) as a 0x-prefixed hex string.
func LabelHash(label string) string { return "0x" + hexEncode(Keccak256([]byte(label))) }
