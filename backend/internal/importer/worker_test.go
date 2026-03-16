package importer

import "testing"

func TestParseYTDLPDownloadedBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		line     string
		want     int64
		wantOkay bool
	}{
		{
			name:     "legacy download prefix format",
			line:     "download:123456",
			want:     123456,
			wantOkay: true,
		},
		{
			name:     "legacy mixed case prefix format",
			line:     "DoWnLoAd: 7890",
			want:     7890,
			wantOkay: true,
		},
		{
			name:     "pure numeric format",
			line:     "138379364",
			want:     138379364,
			wantOkay: true,
		},
		{
			name:     "pure numeric decimal format",
			line:     "138381412.99",
			want:     138381412,
			wantOkay: true,
		},
		{
			name:     "invalid non numeric format",
			line:     "download:abc",
			want:     0,
			wantOkay: false,
		},
		{
			name:     "invalid arbitrary line",
			line:     "something else",
			want:     0,
			wantOkay: false,
		},
		{
			name:     "empty line",
			line:     "",
			want:     0,
			wantOkay: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseYTDLPDownloadedBytes(tc.line)
			if ok != tc.wantOkay {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.wantOkay)
			}
			if got != tc.want {
				t.Fatalf("bytes mismatch: got %d want %d", got, tc.want)
			}
		})
	}
}
