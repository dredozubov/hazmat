//go:build darwin

package hazmat

import "testing"

func TestParseDarwinACLBatchOutputMapsRowsToPaths(t *testing.T) {
	paths := []string{
		"/tmp/hazmat-acl/plain",
		"/tmp/hazmat-acl/with space",
	}
	out := `drwxr-xr-x@ 2 dr  wheel  - 64 Jun 15 19:10 /tmp/hazmat-acl/plain
 0: user:agent allow list,search,readattr
drwxr-xr-x@ 2 dr  wheel  - 64 Jun 15 19:10 /tmp/hazmat-acl/with space
 0: group:dev allow list,add_file,search,file_inherit,directory_inherit
`

	got := parseDarwinACLBatchOutput(out, paths)
	if len(got[paths[0]]) != 1 {
		t.Fatalf("rows for %s = %#v, want one row", paths[0], got[paths[0]])
	}
	if got[paths[0]][0].Principal != "user:agent" {
		t.Fatalf("first principal = %q, want user:agent", got[paths[0]][0].Principal)
	}
	if len(got[paths[1]]) != 1 {
		t.Fatalf("rows for %s = %#v, want one row", paths[1], got[paths[1]])
	}
	if got[paths[1]][0].Principal != "group:dev" {
		t.Fatalf("second principal = %q, want group:dev", got[paths[1]][0].Principal)
	}
	if !got[paths[1]][0].Inherit {
		t.Fatalf("second row Inherit = false, want true")
	}
}
