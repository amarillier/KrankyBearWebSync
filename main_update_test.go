package main

import "testing"

func TestValidateUpdateCheckConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		minDays int
		wantErr bool
	}{
		{name: "valid config", minDays: 1, wantErr: false},
		{name: "always check", minDays: 0, wantErr: false},
		{name: "negative interval", minDays: -1, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateUpdateCheckConfig(tc.minDays)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
