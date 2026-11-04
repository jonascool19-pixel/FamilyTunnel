package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Role       string `json:"role"`
	Listen     string `json:"listen"`
	DataListen string `json:"data_listen"`
	ServerURL  string `json:"server_url"`
	Token      string `json:"token"`
	PublicIP   string `json:"public_ip"`
	CaddyURL   string `json:"caddy_url"`
	Dir        string `json:"-"`
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func writeAtomic(path string, b []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".ft-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func loadConfig(dir string) (Config, error) {
	c := Config{Dir: dir}
	b, e := os.ReadFile(filepath.Join(dir, "config.json"))
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if e != nil {
		return c, e
	}
	if c.Role != "server" && c.Role != "client" {
		return c, errors.New("role must be server or client")
	}
	if len(c.Token) < 32 {
		return c, errors.New("pairing token too short")
	}
	return c, nil
}
func ensureTLS(dir, host string) error {
	cert := filepath.Join(dir, "admin.crt")
	key := filepath.Join(dir, "admin.key")
	if _, e := tls.LoadX509KeyPair(cert, key); e == nil {
		return nil
	}
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return e
	}
	t := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "FamilyTunnel internal admin"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(2, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("10.77.0.1"), net.ParseIP("10.77.0.2")}}
	if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
		t.IPAddresses = append(t.IPAddresses, ip)
	}
	der, e := x509.CreateCertificate(rand.Reader, &t, &t, &k.PublicKey, k)
	if e != nil {
		return e
	}
	if e = writeAtomic(key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}), 0600); e != nil {
		return e
	}
	return writeAtomic(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644)
}
