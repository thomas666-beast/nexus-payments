package rest

// MoneyDTO is the wire format for monetary values.
type MoneyDTO struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// InitiateTransferRequestDTO is the JSON body of POST /v1/transfers.
type InitiateTransferRequestDTO struct {
	FromAccount string   `json:"from_account"`
	ToAccount   string   `json:"to_account"`
	Amount      MoneyDTO `json:"amount"`
	Reason      string   `json:"reason,omitempty"`
}

// InitiateTransferResponseDTO is the JSON response.
type InitiateTransferResponseDTO struct {
	TransferID  string   `json:"transfer_id"`
	Status      string   `json:"status"`
	Amount      MoneyDTO `json:"amount"`
	FromAccount string   `json:"from_account"`
	ToAccount   string   `json:"to_account"`
	EntryIDs    []string `json:"entry_ids"`
}

// TransferStatusResponseDTO is the JSON body of GET /v1/transfers/{id}.
type TransferStatusResponseDTO struct {
	TransferID  string   `json:"transfer_id"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason,omitempty"`
	Amount      MoneyDTO `json:"amount"`
	FromAccount string   `json:"from_account"`
	ToAccount   string   `json:"to_account"`
}

// BalanceResponseDTO is the JSON body of GET /v1/accounts/{id}/balance.
type BalanceResponseDTO struct {
	Balance MoneyDTO `json:"balance"`
}

// ErrorDTO is the standard error response shape.
type ErrorDTO struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}
