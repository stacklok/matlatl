package platform

import "testing"

func TestExitCode_Meaning(t *testing.T) {
	tests := []struct {
		code ExitCode
		want int
		name string
	}{
		{ExitOK, 0, "ok"},
		{ExitFindings, 1, "findings"},
		{ExitUsage, 2, "usage"},
		{ExitRuntime, 3, "runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.code) != tt.want {
				t.Errorf("ExitCode %s = %d, want %d", tt.name, int(tt.code), tt.want)
			}
			if got := tt.code.String(); got != tt.name {
				t.Errorf("ExitCode(%d).String() = %q, want %q", int(tt.code), got, tt.name)
			}
		})
	}
}

func TestBuildInfo_NotEmpty(t *testing.T) {
	if BuildInfo() == "" {
		t.Fatal("platform.BuildInfo() returned empty")
	}
}
