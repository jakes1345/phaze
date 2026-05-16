package updater

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.1.0", "1.0.9", true},
		{"2.0.0", "1.99.99", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"v1.2.3", "1.2.2", true},      // tolerates "v" prefix
		{"1.2.3", "v1.2.2", true},      // either side
		{"1.2.3", "1.2.3-rc1", true},   // release > pre-release of same numerics
		{"1.2.3-rc2", "1.2.3-rc1", false}, // we don't compare pre-release tags
		{"1.2.3-rc1", "1.2.3", false},  // pre-release < release
		{"1.0.0", "", true},            // empty current → notify
		{"", "1.0.0", false},           // empty latest → no upgrade
	}
	for _, tc := range cases {
		got := IsNewer(tc.latest, tc.current)
		if got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestPlatformAssetFor(t *testing.T) {
	m := Manifest{
		Platforms: map[string]PlatformAsset{
			"linux":   {URL: "https://example/linux", SHA256: "abc", Name: "Phaze.linux"},
			"windows": {URL: "", SHA256: "xyz"}, // empty URL → treat as missing
		},
	}
	if a := m.PlatformAssetFor("linux"); a == nil || a.SHA256 != "abc" {
		t.Errorf("linux lookup: got %+v", a)
	}
	if a := m.PlatformAssetFor("windows"); a != nil {
		t.Errorf("windows with empty URL should be nil: %+v", a)
	}
	if a := m.PlatformAssetFor("darwin"); a != nil {
		t.Errorf("missing platform should be nil")
	}

	empty := Manifest{}
	if a := empty.PlatformAssetFor("linux"); a != nil {
		t.Errorf("nil platforms map should yield nil asset")
	}
}

func TestAsNeedsUserAction(t *testing.T) {
	if _, ok := AsNeedsUserAction(nil); ok {
		t.Error("nil err should not be NeedsUserAction")
	}
	wrapped := &NeedsUserActionError{Path: "/tmp/Phaze.apk"}
	if p, ok := AsNeedsUserAction(wrapped); !ok || p != "/tmp/Phaze.apk" {
		t.Errorf("wrapped err: got %q ok=%v", p, ok)
	}
}
