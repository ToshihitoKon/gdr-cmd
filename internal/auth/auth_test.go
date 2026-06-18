package auth

import "testing"

func Test_extractCode_ExtractsCodeFromURLAndVerifiesState(t *testing.T) {
	const state = "expected-state"

	tests := []struct {
		name     string
		input    string
		wantCode string
		wantErr  bool
	}{
		{
			name:     "extract code from redirect URL",
			input:    "http://127.0.0.1:9999/?state=expected-state&code=4/abc123&scope=drive",
			wantCode: "4/abc123",
		},
		{
			name:     "bare authorization code is used as-is",
			input:    "4/xyz789",
			wantCode: "4/xyz789",
		},
		{
			name:     "surrounding whitespace is trimmed",
			input:    "  4/trimmed  ",
			wantCode: "4/trimmed",
		},
		{
			name:    "mismatched state is an error",
			input:   "http://127.0.0.1:9999/?state=wrong&code=4/abc",
			wantErr: true,
		},
		{
			name:    "error query is an error",
			input:   "http://127.0.0.1:9999/?error=access_denied",
			wantErr: true,
		},
		{
			name:    "URL without code is an error",
			input:   "http://127.0.0.1:9999/?state=expected-state",
			wantErr: true,
		},
		{
			name:    "empty input is an error",
			input:   "   ",
			wantErr: true,
		},
		{
			name:     "URL without state is allowed if code is present",
			input:    "http://127.0.0.1:9999/?code=4/nostate",
			wantCode: "4/nostate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractCode(tt.input, state)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected an error but got nil (got code=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantCode {
				t.Errorf("extractCode() = %q, want %q", got, tt.wantCode)
			}
		})
	}
}
