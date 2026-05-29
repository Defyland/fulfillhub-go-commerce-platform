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
