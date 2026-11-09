// Package hederapay pays a relay for a data session with the x402 "exact"
// scheme on Hedera testnet (ADR 0021). It builds a partially signed token
// transfer that the facilitator verifies, completes and submits. The node
// needs the token but no HBAR, because the facilitator pays the network fee.
package hederapay

// NetworkTestnet is the only network the node pays on. Mainnet needs a new
// decision (ADR 0021, D4).
const NetworkTestnet = "hedera:testnet"

const (
	x402Version     = 2
	schemeExact     = "exact"
	headerRequired  = "PAYMENT-REQUIRED"
	headerSignature = "PAYMENT-SIGNATURE"
	headerResponse  = "PAYMENT-RESPONSE"
)

// Requirements is one way to pay, as the relay states it.
type Requirements struct {
	Scheme            string            `json:"scheme"`
	Network           string            `json:"network"`
	Amount            string            `json:"amount"`
	Asset             string            `json:"asset"`
	PayTo             string            `json:"payTo"`
	MaxTimeoutSeconds int               `json:"maxTimeoutSeconds"`
	Extra             map[string]string `json:"extra,omitempty"`
}

// Resource names what is being paid for.
type Resource struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Required is the decoded PAYMENT-REQUIRED header of a 402 response.
type Required struct {
	X402Version int            `json:"x402Version"`
	Error       string         `json:"error,omitempty"`
	Resource    Resource       `json:"resource"`
	Accepts     []Requirements `json:"accepts"`
}

// Payload is what the node sends in the PAYMENT-SIGNATURE header.
type Payload struct {
	X402Version int          `json:"x402Version"`
	Resource    Resource     `json:"resource"`
	Accepted    Requirements `json:"accepted"`
	Payload     struct {
		Transaction string `json:"transaction"`
	} `json:"payload"`
}

// Settlement is the decoded PAYMENT-RESPONSE header.
type Settlement struct {
	Success     bool   `json:"success"`
	ErrorReason string `json:"errorReason,omitempty"`
	Payer       string `json:"payer,omitempty"`
	Transaction string `json:"transaction"`
	Network     string `json:"network"`
}
