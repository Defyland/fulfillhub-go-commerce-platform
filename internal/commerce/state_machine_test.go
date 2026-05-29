package commerce

import (
	"errors"
	"testing"
)

func TestValidOrderStatusesContainsEveryRuntimeStatus(t *testing.T) {
	got := map[OrderStatus]bool{}
	for _, status := range ValidOrderStatuses() {
		got[status] = true
	}

	for _, status := range []OrderStatus{
		StatusPendingFulfillment,
		StatusInventoryReserved,
		StatusPaymentAuthorized,
		StatusShipmentCreated,
		StatusCancellationPending,
		StatusManualReview,
		StatusCancelled,
		StatusCompleted,
		StatusFailed,
	} {
		if !got[status] {
			t.Fatalf("ValidOrderStatuses missing %q", status)
		}
	}
}

func TestValidateOrderTransitionAllowsSagaAndCancellationPaths(t *testing.T) {
	for _, tc := range []struct {
		from OrderStatus
		to   OrderStatus
	}{
		{StatusPendingFulfillment, StatusInventoryReserved},
		{StatusInventoryReserved, StatusPaymentAuthorized},
		{StatusPaymentAuthorized, StatusShipmentCreated},
		{StatusShipmentCreated, StatusCompleted},
		{StatusPendingFulfillment, StatusCancellationPending},
		{StatusInventoryReserved, StatusCancellationPending},
		{StatusPaymentAuthorized, StatusCancellationPending},
		{StatusShipmentCreated, StatusCancellationPending},
		{StatusCancellationPending, StatusCancelled},
		{StatusCancellationPending, StatusManualReview},
		{StatusPendingFulfillment, StatusFailed},
		{StatusInventoryReserved, StatusCancelled},
		{StatusCompleted, StatusCompleted},
	} {
		if err := ValidateOrderTransition(tc.from, tc.to); err != nil {
			t.Fatalf("ValidateOrderTransition(%q, %q) returned error: %v", tc.from, tc.to, err)
		}
	}
}

func TestValidateOrderTransitionRejectsInvalidLifecycleMoves(t *testing.T) {
	for _, tc := range []struct {
		from OrderStatus
		to   OrderStatus
	}{
		{StatusPendingFulfillment, StatusCompleted},
		{StatusCompleted, StatusCancelled},
		{StatusCancelled, StatusPaymentAuthorized},
		{StatusManualReview, StatusCompleted},
		{StatusFailed, StatusInventoryReserved},
		{"unknown", StatusCompleted},
		{StatusPendingFulfillment, "archived"},
	} {
		err := ValidateOrderTransition(tc.from, tc.to)
		if !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("ValidateOrderTransition(%q, %q) = %v, want ErrInvalidStateTransition", tc.from, tc.to, err)
		}
	}
}
