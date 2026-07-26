package auth

import "testing"

func TestParseManualRedirect(t *testing.T) {
	const state = "abc123"

	tests := []struct {
		name    string
		pasted  string
		want    string
		wantErr bool
	}{
		{
			name:   "full redirect URL",
			pasted: "http://127.0.0.1:8888/callback?code=THECODE&state=abc123",
			want:   "THECODE",
		},
		{
			name:   "surrounding whitespace and newline",
			pasted: "  http://127.0.0.1:8888/callback?code=THECODE&state=abc123  \n",
			want:   "THECODE",
		},
		{
			name:   "bare code",
			pasted: "THECODE\n",
			want:   "THECODE",
		},
		{
			name:    "state mismatch is rejected",
			pasted:  "http://127.0.0.1:8888/callback?code=THECODE&state=wrong",
			wantErr: true,
		},
		{
			name:    "authorization denied is surfaced",
			pasted:  "http://127.0.0.1:8888/callback?error=access_denied&state=abc123",
			wantErr: true,
		},
		{
			name:    "URL with no code",
			pasted:  "http://127.0.0.1:8888/callback?state=abc123",
			wantErr: true,
		},
		{
			name:    "empty input",
			pasted:  "   \n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseManualRedirect(tt.pasted, state)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got code %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}
