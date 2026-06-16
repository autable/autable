package workflow

import "testing"

func TestNodeInfoCanDescribeTrigger(t *testing.T) {
	info := NodeInfo{
		Type:        "table.record.changed",
		DisplayName: "Record changed",
		Stateless:   true,
		Trigger:     true,
		Outputs: []Port{{
			Name:     "record",
			Type:     "TriggerRecord",
			Required: true,
		}},
	}

	if !info.Stateless || !info.Trigger {
		t.Fatalf("unexpected trigger info: %#v", info)
	}
	if info.Outputs[0].Name != "record" {
		t.Fatalf("unexpected output port: %#v", info.Outputs[0])
	}
}
