package nodes

import (
	"testing"

	"autable/internal/history"
)

func TestRemoteNodesAreServerNodesWithoutTriggers(t *testing.T) {
	all := map[string]bool{}
	for _, node := range All(Dependencies{History: history.NewMemoryStore()}) {
		all[node.Info().Type] = true
	}
	remote := Remote()
	if len(remote) == 0 {
		t.Fatal("expected remote-capable nodes")
	}
	for _, node := range remote {
		info := node.Info()
		if !all[info.Type] {
			t.Fatalf("remote node %q is not registered on the server", info.Type)
		}
		if info.Trigger {
			t.Fatalf("trigger node %q cannot be remote-capable", info.Type)
		}
	}
}
