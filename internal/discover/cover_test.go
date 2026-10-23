package discover

import "testing"

// pickCover 的五档回落顺序，以及缺档时是否会漏。
func TestPickCoverFallback(t *testing.T) {
	cases := []struct {
		name   string
		images map[string]string
		want   string
	}{
		{"只有 common", map[string]string{"common": "c"}, "c"},
		{"缺 common 回落 large", map[string]string{"large": "l"}, "l"},
		{"缺 common/large 回落 medium", map[string]string{"medium": "m"}, "m"},
		{"缺三档回落 small", map[string]string{"small": "s"}, "s"},
		{"只剩 grid", map[string]string{"grid": "g"}, "g"},
		{"common 是空白串", map[string]string{"common": "  ", "large": "l"}, "l"},
		{"全空", map[string]string{}, ""},
		{"nil", nil, ""},
		{"五档齐全", map[string]string{"common": "c", "large": "l", "medium": "m", "small": "s", "grid": "g"}, "c"},
		{"未知尺寸名", map[string]string{"huge": "h"}, ""},
	}
	for _, tc := range cases {
		got := pickCover(tc.images)
		t.Logf("%s: pickCover = %q", tc.name, got)
		if got != tc.want {
			t.Errorf("%s: pickCover(%v) = %q, want %q", tc.name, tc.images, got, tc.want)
		}
	}
}
