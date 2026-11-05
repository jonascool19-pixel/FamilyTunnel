package app

import "testing"

func TestScanIPv4sIsBounded(t *testing.T) {
	ips, err := scanIPv4s("192.168.178.50")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 253 {
		t.Fatalf("got %d scan targets", len(ips))
	}
	for _, ip := range ips {
		if ip == "192.168.178.50" {
			t.Fatal("admin address must not be scanned")
		}
	}
}

func TestScanIPv4sRejectsPublicAddress(t *testing.T) {
	if _, err := scanIPv4s("203.0.113.10"); err == nil {
		t.Fatal("public address accepted")
	}
}
