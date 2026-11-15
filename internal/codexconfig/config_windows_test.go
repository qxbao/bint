//go:build windows

package codexconfig

import "testing"

func TestMarkerPathWindows(t *testing.T) {
	for _, tc := range []struct{ dir, marker, want string }{
		{`C:\repo\child`, `.git`, `C:\repo\child\.git`},
		{`C:\repo\child`, `C:\.git`, `C:\.git`},
		{`C:\repo\child`, `D:\marker`, `D:\marker`},
		{`C:\repo\child`, `\marker`, `C:\marker`},
		{`C:\repo\child`, `/marker`, `C:/marker`},
		{`C:\repo\child`, `C:marker`, `C:marker`},
		{`C:\repo\child`, `D:marker`, `D:marker`},
		{`C:\repo\child`, `\\server\share\marker`, `\\server\share\marker`},
		{`\\server\share\repo`, `\marker`, `\\server\share\marker`},
	} {
		t.Run(tc.dir+"/"+tc.marker, func(t *testing.T) {
			if got := markerPath(tc.dir, tc.marker); got != tc.want {
				t.Fatalf("markerPath(%q, %q) = %q; want %q", tc.dir, tc.marker, got, tc.want)
			}
		})
	}
}
