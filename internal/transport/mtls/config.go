// Package mtls настраивает взаимную TLS-аутентификацию и точные полномочия сервисов.
package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
)

// Config задаёт файлы собственной пары ключ/сертификат и доверенного CA, без plaintext режима.
type Config struct{ CertFile, KeyFile, CAFile string }

// loadCredentials читает файлы без их содержимого или пути в возвращаемой ошибке.
func loadCredentials(c Config) (tls.Certificate, *x509.CertPool, error) {
	pair, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return tls.Certificate{}, nil, errors.New("cannot load TLS certificate/key")
	}
	data, err := os.ReadFile(c.CAFile)
	if err != nil {
		return tls.Certificate{}, nil, errors.New("cannot load TLS CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return tls.Certificate{}, nil, errors.New("invalid TLS CA")
	}
	return pair, pool, nil
}

// ServerConfig требует клиентский сертификат, проверяет CA, EKU и срок; SAN проверяет policy.
func ServerConfig(c Config) (*tls.Config, error) {
	pair, pool, err := loadCredentials(c)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}

// ClientConfig включает проверку точного serverName и собственного сертификата сервиса.
func ClientConfig(c Config, serverName string) (*tls.Config, error) {
	if serverName == "" || strings.TrimSpace(serverName) != serverName || strings.ContainsAny(serverName, "*/ :\t\r\n") {
		return nil, errors.New("invalid TLS server name")
	}
	pair, pool, err := loadCredentials(c)
	if err != nil {
		return nil, err
	}
	return ForServer(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: pool}, serverName), nil
}

// ForServer копирует credentials и требует точный DNS SAN, исключая wildcard-удостоверения.
func ForServer(base *tls.Config, name string) *tls.Config {
	config := base.Clone()
	config.ServerName = name
	config.VerifyConnection = func(state tls.ConnectionState) error {
		return verifyDNSName(state, name)
	}
	return config
}

// verifyDNSName проверяет только имена уже подтверждённого leaf сертификата.
func verifyDNSName(state tls.ConnectionState, name string) error {
	if name != "" && len(state.VerifiedChains) > 0 && len(state.PeerCertificates) > 0 {
		for _, san := range state.PeerCertificates[0].DNSNames {
			if san == name {
				return nil
			}
		}
	}
	return errors.New("unexpected TLS peer identity")
}
