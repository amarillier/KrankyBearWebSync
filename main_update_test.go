package main

import "testing"

func TestValidateUpdateCheckConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		owner   string
		repo    string
		minDays int
		wantErr bool
	}{
		{name: "valid config", owner: "amarillier", repo: "KrankyBearWebSync", minDays: 1, wantErr: false},
		{name: "missing owner", owner: "", repo: "KrankyBearWebSync", minDays: 1, wantErr: true},
		{name: "missing repo", owner: "amarillier", repo: "", minDays: 1, wantErr: true},
		{name: "negative interval", owner: "amarillier", repo: "KrankyBearWebSync", minDays: -1, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateUpdateCheckConfig(tc.owner, tc.repo, tc.minDays)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
