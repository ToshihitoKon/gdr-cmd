package auth

import "testing"

func Test_extractCode_URLからコードを抽出しstateを検証する(t *testing.T) {
	const state = "expected-state"

	tests := []struct {
		name     string
		input    string
		wantCode string
		wantErr  bool
	}{
		{
			name:     "リダイレクトURLからcodeを抽出",
			input:    "http://127.0.0.1:9999/?state=expected-state&code=4/abc123&scope=drive",
			wantCode: "4/abc123",
		},
		{
			name:     "素の認可コードはそのまま使う",
			input:    "4/xyz789",
			wantCode: "4/xyz789",
		},
		{
			name:     "前後の空白を除去する",
			input:    "  4/trimmed  ",
			wantCode: "4/trimmed",
		},
		{
			name:    "stateが一致しないとエラー",
			input:   "http://127.0.0.1:9999/?state=wrong&code=4/abc",
			wantErr: true,
		},
		{
			name:    "errorクエリがあるとエラー",
			input:   "http://127.0.0.1:9999/?error=access_denied",
			wantErr: true,
		},
		{
			name:    "codeが無いURLはエラー",
			input:   "http://127.0.0.1:9999/?state=expected-state",
			wantErr: true,
		},
		{
			name:    "空入力はエラー",
			input:   "   ",
			wantErr: true,
		},
		{
			name:     "stateが無いURLでもcodeがあれば許容",
			input:    "http://127.0.0.1:9999/?code=4/nostate",
			wantCode: "4/nostate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractCode(tt.input, state)
			if tt.wantErr {
				if err == nil {
					t.Errorf("エラーを期待したが nil (got code=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got != tt.wantCode {
				t.Errorf("extractCode() = %q, want %q", got, tt.wantCode)
			}
		})
	}
}
