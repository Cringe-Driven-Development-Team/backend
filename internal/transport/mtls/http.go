package mtls

import (
	"crypto/tls"
	"errors"
)

// HTTPServerConfig требует проверенную цепочку и точное DNS-имя одного из
// разрешённых вызывающих сервисов. clientNames — допустимые SAN клиентского
// сертификата; пустой список отклоняет любого клиента.
func HTTPServerConfig(cfg Config, clientNames ...string) (*tls.Config, error) {
	config, err := ServerConfig(cfg)
	if err != nil {
		return nil, err
	}
	config.VerifyConnection = func(state tls.ConnectionState) error {
		return verifyAnyDNSName(state, clientNames)
	}
	return config, nil
}

// verifyAnyDNSName принимает проверенного клиента, если его сертификат несёт
// одно из имён names.
func verifyAnyDNSName(state tls.ConnectionState, names []string) error {
	for _, name := range names {
		if verifyDNSName(state, name) == nil {
			return nil
		}
	}
	return errors.New("unexpected TLS peer identity")
}
