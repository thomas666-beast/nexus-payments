package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

// canonicalRequest is the deterministic shape we hash for idempotency.
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
type storedResult struct {
	TransferID  domain.TransferID `json:"transfer_id"`
	FromAccount domain.AccountID  `json:"from_account"`
	ToAccount   domain.AccountID  `json:"to_account"`
	AmountMinor int64             `json:"amount_minor"`
	Currency    domain.Currency   `json:"currency"`
	Status      string            `json:"status"`
	EntryIDs    []domain.EntryID  `json:"entry_ids"`
}

// storedError is the serialized form of a business failure.
type storedError struct {
	Message string `json:"message"`
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

func encodeError(err error) ([]byte, error) {
	return json.Marshal(storedError{Message: err.Error()})
}

func decodeResult(body []byte) (ExecuteResult, error) {
	var sr storedResult
	if err := json.Unmarshal(body, &sr); err != nil {
		return ExecuteResult{}, fmt.Errorf("decode stored result: %w", err)
	}
	res := ExecuteResult{
		Transfer: domain.Transfer{
			ID:          sr.TransferID,
			FromAccount: sr.FromAccount,
			ToAccount:   sr.ToAccount,
			Amount:      domain.Money{Amount: sr.AmountMinor, Currency: sr.Currency},
			Status:      domain.TransferStatus(sr.Status),
		},
	}
	for _, id := range sr.EntryIDs {
		res.Entries = append(res.Entries, domain.LedgerEntry{ID: id})
	}
	return res, nil
}
