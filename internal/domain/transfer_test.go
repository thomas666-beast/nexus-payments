package domain

import (
	"testing"
	"time"
)

func TestCanTransitionTo_AllPairsAgainstMap(t *testing.T) {
	all := []TransferStatus{
		TransferStatusInitiated,
		TransferStatusValidated,
		TransferStatusSettled,
		TransferStatusFailed,
	}
	for _, from := range all {
		for _, to := range all {
			want := false
			if targets, ok := allowedTransitions[from]; ok {
				_, want = targets[to]
			}
			if got := CanTransitionTo(from, to); got != want {
				t.Fatalf("CanTransitionTo(%s,%s)=%v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTransferStatus_IsTerminal(t *testing.T) {
	if !TransferStatusSettled.IsTerminal() {
		t.Fatal("SETTLED must be terminal")
	}
	if !TransferStatusFailed.IsTerminal() {
		t.Fatal("FAILED must be terminal")
	}
	if TransferStatusInitiated.IsTerminal() {
		t.Fatal("INITIATED must not be terminal")
	}
}

func TestTransfer_Validate_RejectsSelfTransfer(t *testing.T) {
	tx := Transfer{
		ID:          NewTransferID(),
		FromAccount: "A",
		ToAccount:   "A",
		Amount:      MustMoney(100, USD),
		Status:      TransferStatusInitiated,
		CreatedAt:   time.Now(),
	}
	if err := tx.Validate(); err == nil {
		t.Fatal("self transfer must be rejected")
	}
}

func TestTransfer_Validate_RejectsNonPositiveAmount(t *testing.T) {
	tx := Transfer{
		ID:          NewTransferID(),
		FromAccount: "A",
		ToAccount:   "B",
		Amount:      MustMoney(0, USD),
		Status:      TransferStatusInitiated,
		CreatedAt:   time.Now(),
	}
	if err := tx.Validate(); err == nil {
		t.Fatal("zero amount must be rejected")
	}
}
