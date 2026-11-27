"""Создавайте одноразовый CA для настоящих TLS gRPC tests без внешних PKI."""

from datetime import UTC, datetime, timedelta

import grpc
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID


class TestPKI:
    """Выпускайте сертификаты только для тестового временного каталога."""

    __test__ = False

    def __init__(self, directory):
        """Создайте доверенный CA, не затрагивая системное trust store."""
        self.directory = directory
        self.key = ec.generate_private_key(ec.SECP256R1())
        name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Cellestial test CA")])
        self.ca = (
            x509.CertificateBuilder()
            .subject_name(name)
            .issuer_name(name)
            .public_key(self.key.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(datetime.now(UTC) - timedelta(hours=1))
            .not_valid_after(datetime.now(UTC) + timedelta(days=1))
            .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
            .sign(self.key, algorithm=hashes.SHA256())
        )
        self.ca_bytes = self.ca.public_bytes(serialization.Encoding.PEM)
        self.ca_file = directory / "ca.pem"
        self.ca_file.write_bytes(self.ca_bytes)

    def certificate(self, name, *, server=False, expired=False):
        """Верните PEM key/cert с одним DNS SAN и назначенным EKU."""
        key = ec.generate_private_key(ec.SECP256R1())
        cert = (
            x509.CertificateBuilder()
            .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "unused CN")]))
            .issuer_name(self.ca.subject)
            .public_key(key.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(datetime.now(UTC) - timedelta(hours=2))
            .not_valid_after(datetime.now(UTC) + timedelta(hours=-1 if expired else 2))
            .add_extension(x509.SubjectAlternativeName([x509.DNSName(name)]), critical=False)
            .add_extension(
                x509.ExtendedKeyUsage(
                    [ExtendedKeyUsageOID.SERVER_AUTH if server else ExtendedKeyUsageOID.CLIENT_AUTH]
                ),
                critical=False,
            )
            .sign(self.key, algorithm=hashes.SHA256())
        )
        return (
            key.private_bytes(
                serialization.Encoding.PEM,
                serialization.PrivateFormat.PKCS8,
                serialization.NoEncryption(),
            ),
            cert.public_bytes(serialization.Encoding.PEM),
        )

    def channel_credentials(self, name, *, expired=False):
        """Подключите сертификат клиента к отдельному gRPC channel."""
        key, cert = self.certificate(name, expired=expired)
        return grpc.ssl_channel_credentials(self.ca_bytes, key, cert)
