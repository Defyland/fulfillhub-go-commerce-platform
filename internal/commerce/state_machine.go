package commerce

import "fmt"

func ValidOrderStatuses() []OrderStatus {
	return []OrderStatus{
		StatusPendingFulfillment,
		StatusInventoryReserved,
		StatusPaymentAuthorized,
		StatusShipmentCreated,
		StatusCancellationPending,
		StatusManualReview,
		StatusCancelled,
		StatusCompleted,
		StatusFailed,
	}
}

func IsValidOrderStatus(status OrderStatus) bool {
	for _, valid := range ValidOrderStatuses() {
		if status == valid {
			return true
		}
	}
	return false
}

func CanTransitionOrderStatus(from, to OrderStatus) bool {
	if from == to {
		return IsValidOrderStatus(to)
	}

	switch from {
	case StatusPendingFulfillment:
		switch to {
		case StatusInventoryReserved, StatusCancellationPending, StatusFailed:
			return true
		}
	case StatusInventoryReserved:
		switch to {
		case StatusPaymentAuthorized, StatusCancellationPending, StatusCancelled:
			return true
		}
	case StatusPaymentAuthorized:
		switch to {
		case StatusShipmentCreated, StatusCancellationPending:
			return true
		}
	case StatusShipmentCreated:
		switch to {
		case StatusCompleted, StatusCancellationPending:
			return true
		}
	case StatusCancellationPending:
		switch to {
		case StatusCancelled, StatusManualReview:
			return true
		}
	}

	return false
}

func ValidateOrderTransition(from, to OrderStatus) error {
	if !IsValidOrderStatus(from) {
		return fmt.Errorf("%w: unknown source order status %q", ErrInvalidStateTransition, from)
	}
	if !IsValidOrderStatus(to) {
		return fmt.Errorf("%w: unknown target order status %q", ErrInvalidStateTransition, to)
	}
	if !CanTransitionOrderStatus(from, to) {
		return fmt.Errorf("%w: cannot move order from %q to %q", ErrInvalidStateTransition, from, to)
	}
	return nil
}
