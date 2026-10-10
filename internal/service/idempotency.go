package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

// canonicalRequest is the deterministic shape we hash for idempotency.
// Deliberately excludes Reason — a caller adding a note should not
// create a new logical transfer.
type canonicalRequest struct {
	FromAccount domain.AccountID `json:"from_account"`
	ToAccount   domain.AccountID `json:"to_account"`
	AmountMinor int64            `json:"amount_minor"`
	Currency    domain.Currency  `json:"currency"`
}

// hashRequest produces a stable SHA-256 hex digest of the request.
func hashRequest(req ExecuteRequest) (string, error) {
	c := canonicalRequest{
		FromAccount: req.FromAccount,
		ToAccount:   req.ToAccount,
		AmountMinor: req.Amount.Amount,
		Currency:    req.Amount.Currency,
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// storedResult is the serialized form of a successful ExecuteResult.
// Kept separate from ExecuteResult so we control the wire format.
type storedResult struct {
	TransferID  domain.TransferID `json:"transfer_id"`
	FromAccount domain.AccountID  `json:"from_account"`
	ToAccount   domain.AccountID  `json:"to_account"`
	AmountMinor int64             `json:"amount_minor"`
	Currency    domain.Currency   `json:"currency"`
	Status      string            `json:"status"`
	EntryIDs    []domain.EntryID  `json:"entry_ids"`
}

func encodeResult(r ExecuteResult) ([]byte, error) {
	sr := storedResult{
		TransferID:  r.Transfer.ID,
		FromAccount: r.Transfer.FromAccount,
		ToAccount:   r.Transfer.ToAccount,
		AmountMinor: r.Transfer.Amount.Amount,
		Currency:    r.Transfer.Amount.Currency,
		Status:      string(r.Transfer.Status),
	}
	for _, e := range r.Entries {
		sr.EntryIDs = append(sr.EntryIDs, e.ID)
	}
	return json.Marshal(sr)
}

// storedError is the serialized form of a business failure.
type storedError struct {
	Message string `json:"message"`
}

func encodeError(err error) ([]byte, error) {
	return json.Marshal(storedError{Message: err.Error()})
}
