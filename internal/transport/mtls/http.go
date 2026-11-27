package mtls

import "crypto/tls"

// HTTPServerConfig требует проверенную цепочку и точное имя вызывающего HTTP-сервиса.
func HTTPServerConfig(cfg Config, clientName string) (*tls.Config, error) {
	config, err := ServerConfig(cfg)
	if err != nil {
		return nil, err
	}
	config.VerifyConnection = func(state tls.ConnectionState) error {
		return verifyDNSName(state, clientName)
	}
	return config, nil
}
