package supervisor

import "testing"

func TestACLOpensWeb(t *testing.T) {
	closed := []byte("http_access allow inbound\nhttp_access allow vendorhost\nhttp_access allow probe\nhttp_access allow probe_dst\n# http_access allow container_out\n")
	if ACLOpensWeb(closed) {
		t.Fatal("stand-in allows opened the web")
	}
	if !ACLOpensWeb([]byte("http_access allow container_out\n")) {
		t.Fatal("container_out did not open the web")
	}
	if !ACLOpensWeb([]byte("http_access allow container_tls\n")) {
		t.Fatal("container_tls did not open the web")
	}
	if !ACLOpensWeb([]byte("http_access allow all\n")) {
		t.Fatal("allow all did not open the web")
	}
	// The ACL name is the token, so !inbound is not inbound.
	if !ACLOpensWeb([]byte("http_access allow !inbound\n")) {
		t.Fatal("allow !inbound did not open the web")
	}
	if ACLOpensWeb([]byte("http_access allow inbound X\n")) {
		t.Fatal("allow inbound X opened the web")
	}
	// Fields splits on spaces and tabs after TrimSpace.
	for _, line := range []string{
		"  http_access allow container_out\n",
		"\thttp_access allow container_out\n",
		" \t http_access allow container_out\n",
	} {
		if !ACLOpensWeb([]byte(line)) {
			t.Fatalf("whitespace did not open the web: %q", line)
		}
	}
}
