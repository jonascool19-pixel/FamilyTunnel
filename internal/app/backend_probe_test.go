package app

import (
	"context"
	"net"
	"testing"
)

func TestProbeBackend(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := "http://" + listener.Addr().String()
	if result := probeBackend(context.Background(), address); result["reachable"] != true {
		t.Fatalf("open listener: %v", result)
	}
	listener.Close()
	if result := probeBackend(context.Background(), address); result["reachable"] != false {
		t.Fatalf("closed listener: %v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := probeBackend(ctx, address); result["reachable"] != false {
		t.Fatalf("cancelled: %v", result)
	}
}
