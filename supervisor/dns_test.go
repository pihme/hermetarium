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
}
