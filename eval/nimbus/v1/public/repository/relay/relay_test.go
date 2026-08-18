package relay

import "testing"

func TestNormalChecks(t *testing.T) {
	if AdmitBatch(256) != nil || AdmitBatch(257) == nil {
		t.Fatal("batch bound")
	}
	if ValidateWebhook("https://203.0.113.8/hook", false) != nil {
		t.Fatal("public HTTPS")
	}
	if PlaceTenant("acme", []string{"a", "b"}) == "" {
		t.Fatal("placement")
	}
	var l Lifecycle
	if !l.BeginSession() {
		t.Fatal("session")
	}
	l.Drain()
	if l.Ready() || !l.Live() || l.BeginSession() {
		t.Fatal("drain")
	}
	l.EndSession()
}
