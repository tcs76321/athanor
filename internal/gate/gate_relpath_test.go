// Internal-only test for the helper functions added alongside the
// M4-T1 Gate G1 extension. The main gate test exercises the
// positive path; this file pins the helper contracts (path
// normalization, sorted identifier list) so a future refactor
// cannot silently change the repo-relative path shape or the
// error-message format without breaking this test.
package gate

import (
	"reflect"
	"testing"
)

func TestRelPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "internal file at the root",
			in:   "../../internal/foo/bar.go",
			want: "internal/foo/bar.go",
		},
		{
			name: "internal deep file",
			in:   "../../internal/airlock/paths/paths_linux.go",
			want: "internal/airlock/paths/paths_linux.go",
		},
		{
			name: "cmd file",
			in:   "../../cmd/athanor/main.go",
			want: "cmd/athanor/main.go",
		},
		{
			name: "no internal-or-cmd segment returns cleaned input unchanged",
			in:   "../../oops.txt",
			want: "../../oops.txt", // filepath.Clean preserves leading ../ segments
		},
		{
			name: "first segment wins on internal/cmd ambiguity",
			in:   "../../cmd/with/internal/inside.go",
			want: "cmd/with/internal/inside.go",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := relPath(tc.in)
			if got != tc.want {
				t.Errorf("relPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAllowedInternalSyscallIdentsKeyList(t *testing.T) {
	got := allowedInternalSyscallIdentsKeyList()
	want := []string{"O_NOFOLLOW"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("allowedInternalSyscallIdentsKeyList() = %v, want %v", got, want)
	}
	// Sorted invariant: any future identifier must be added to the
	// expected slice in the correct sort position. The test fails
	// before the Gate G1 walk would notice a missing entry.
}
