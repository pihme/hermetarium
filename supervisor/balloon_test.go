package supervisor

import "testing"

func TestFreePageBalloon(t *testing.T) {
	b := freePageBalloon()
	if b["amount_mib"] != 0 || b["deflate_on_oom"] != false || b["stats_polling_interval_s"] != 0 {
		t.Fatalf("balloon takes memory or polls stats: %#v", b)
	}
	if b["free_page_reporting"] != true {
		t.Fatal("free page reporting is off")
	}
}
