package app

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
)

// Pins are trusted only after the user selects the discovered LAN service.
// Subsequent connections must present this exact certificate; no automatic repinning.
func pinnedTLS(pin string) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return fmt.Errorf("Lokales TLS-Zertifikat fehlt")
		}
		sum := sha256.Sum256(state.PeerCertificates[0].Raw)
		if hex.EncodeToString(sum[:]) != pin {
			return fmt.Errorf("Lokales TLS-Zertifikat hat sich geändert; Dienst erneut prüfen")
		}
		return nil
	}}
}
func backendHTTPTransport(pin string) *http.Transport {
	transport := backendTransport.Clone()
	if pin != "" {
		transport.TLSClientConfig = pinnedTLS(pin)
	}
	return transport
}
func backendNamedTransport(pin, serverName string) *http.Transport {
	transport := backendHTTPTransport(pin)
	if serverName != "" {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		transport.TLSClientConfig.ServerName = serverName
	}
	return transport
}
