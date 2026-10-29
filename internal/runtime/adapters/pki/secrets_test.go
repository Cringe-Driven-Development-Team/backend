package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func newTestCA(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	return newTestCert(t, true)
}

func newTestCert(t *testing.T, isCA bool) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})
}

func newSecrets(t *testing.T, now time.Time) (*Secrets, []byte) {
	t.Helper()
	caCert, caKey := newTestCA(t)
	s, err := New(make([]byte, 32), caCert, caKey, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	return s, caCert
}

func TestTokenIs64Hex(t *testing.T) {
	s, _ := newSecrets(t, time.Now())
	a, err := s.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(a) {
		t.Fatalf("bad token %q", a)
	}
	b, _ := s.NewToken()
	if a == b {
		t.Fatal("tokens must differ")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, _ := newSecrets(t, time.Now())
	c, err := s.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Open(c)
	if err != nil || string(p) != "secret" {
		t.Fatalf("got %q, %v", p, err)
	}
}

func TestOpenTamperedFails(t *testing.T) {
	s, _ := newSecrets(t, time.Now())
	c, _ := s.Seal([]byte("secret"))
	c[len(c)-1] ^= 1
	if _, err := s.Open(c); err == nil {
		t.Fatal("expected error")
	}
	if _, err := s.Open([]byte{1, 2}); err == nil {
		t.Fatal("expected error on short input")
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	caCert, caKey := newTestCA(t)
	if _, err := New(make([]byte, 16), caCert, caKey, fixedClock{time.Now()}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRejectsMismatchedCAKey(t *testing.T) {
	caCert, _ := newTestCA(t)
	_, otherKey := newTestCA(t)
	_, err := New(make([]byte, 32), caCert, otherKey, fixedClock{time.Now()})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewRejectsNonCACert(t *testing.T) {
	cert, key := newTestCert(t, false)
	_, err := New(make([]byte, 32), cert, key, fixedClock{time.Now()})
	if err == nil || !strings.Contains(err.Error(), "not a CA") {
		t.Fatalf("err = %v", err)
	}
}

func TestHostKeyEd25519(t *testing.T) {
	s, _ := newSecrets(t, time.Now())
	priv, pub, err := s.NewHostKey()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if signer.PublicKey().Type() != "ssh-ed25519" {
		t.Fatal(signer.PublicKey().Type())
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") || strings.HasSuffix(pub, "\n") {
		t.Fatalf("bad pub %q", pub)
	}
}

func TestServerCertSignedByCA(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s, caPEM := newSecrets(t, now)
	certPEM, keyPEM, err := s.IssueServerCert("10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("ca")
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots: pool, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatal(err)
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "10.0.0.5" {
		t.Fatalf("ips %v", cert.IPAddresses)
	}
	if !cert.NotAfter.Equal(now.Add(7 * 24 * time.Hour)) {
		t.Fatalf("notAfter %v", cert.NotAfter)
	}
	if kb, _ := pem.Decode(keyPEM); kb == nil {
		t.Fatal("key pem")
	}
	if _, _, err := s.IssueServerCert("not-ip"); err == nil {
		t.Fatal("expected error")
	}
}
