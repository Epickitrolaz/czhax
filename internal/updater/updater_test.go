package updater

import "testing"

func TestAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
		wantErr            bool
	}{
		{"linux", "arm64", "czhax-linux-arm64", false},
		{"linux", "arm", "czhax-linux-armv7", false},
		{"linux", "amd64", "czhax-linux-amd64", false},
		{"darwin", "arm64", "", true},
	}
	for _, tc := range cases {
		got, err := AssetName(tc.goos, tc.goarch)
		if (err != nil) != tc.wantErr {
			t.Fatalf("AssetName(%s,%s) err=%v wantErr=%v", tc.goos, tc.goarch, err, tc.wantErr)
		}
		if got != tc.want {
			t.Fatalf("AssetName(%s,%s) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	body := "abc123  czhax-linux-amd64\ndef456  czhax-linux-arm64\n"
	got, ok := ParseChecksum(body, "czhax-linux-arm64")
	if !ok || got != "def456" {
		t.Fatalf("ParseChecksum failed: got=%q ok=%v", got, ok)
	}
}
