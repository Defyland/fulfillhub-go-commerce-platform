package messaging

import "testing"

func TestQueueTopologiesDefineRetryQueuesAndDLQs(t *testing.T) {
	topologies := QueueTopologies()
	if len(topologies) != len(QueueNames()) {
		t.Fatalf("topologies = %d, want one per queue %d", len(topologies), len(QueueNames()))
	}

	seen := map[string]bool{}
	for _, topology := range topologies {
		if topology.Queue == "" {
			t.Fatal("queue name must be present")
		}
		if len(topology.RoutingKeys) == 0 {
			t.Fatalf("queue %s must define routing keys", topology.Queue)
		}
		if topology.RetryQueue == "" {
			t.Fatalf("queue %s must define retry queue", topology.Queue)
		}
		if topology.RetryTTL <= 0 {
			t.Fatalf("queue %s retry ttl = %s, want positive", topology.Queue, topology.RetryTTL)
		}
		if topology.DLQ == "" {
			t.Fatalf("queue %s must define dlq", topology.Queue)
		}
		seen[topology.Queue] = true
	}

	for _, queue := range QueueNames() {
		if !seen[queue] {
			t.Fatalf("queue %s missing topology", queue)
		}
	}
}

func TestQueueTopologiesRouteCancellationRequests(t *testing.T) {
	topology, ok := findTopology(OrdersCancelQueue)
	if !ok {
		t.Fatal("orders cancellation queue topology missing")
	}
	if !contains(topology.RoutingKeys, "order.cancel_requested") {
		t.Fatalf("orders cancellation routing keys = %v, want order.cancel_requested", topology.RoutingKeys)
	}
	if topology.RetryQueue != "orders.cancel.retry.15s" {
		t.Fatalf("orders cancellation retry queue = %q", topology.RetryQueue)
	}
	if topology.DLQ != "orders.cancel.dlq" {
		t.Fatalf("orders cancellation dlq = %q", topology.DLQ)
	}
}

func TestQueueTopologiesRouteFailureNotifications(t *testing.T) {
	topology, ok := findTopology(NotificationsEmailQueue)
	if !ok {
		t.Fatal("notifications queue topology missing")
	}
	for _, routingKey := range []string{"inventory.rejected", "payment.failed", "shipment.failed"} {
		if !contains(topology.RoutingKeys, routingKey) {
			t.Fatalf("notifications routing keys = %v, want %s", topology.RoutingKeys, routingKey)
		}
	}
}

func findTopology(queue string) (QueueTopology, bool) {
	for _, topology := range QueueTopologies() {
		if topology.Queue == queue {
			return topology, true
		}
	}
	return QueueTopology{}, false
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
