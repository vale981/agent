package apiserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/indihub-space/agent/logutil"
)

const (
	tlsDirName = "./.indihub-agent/tls"

	rootKeyCA  = tlsDirName + "/root_CA.key"
	rootCertCA = tlsDirName + "/root_CA.pem"
	serverKey  = tlsDirName + "/server.key"
	serverCert = tlsDirName + "/server.pem"
)

func getSelfSignedCert() (string, string, error) {
	// create dir for tls keys and certificates
	if _, err := os.Stat(tlsDirName); os.IsNotExist(err) {
		if err := os.MkdirAll(tlsDirName, 0700); err != nil {
			return "", "", err
		}
	}
	// return if files are already there
	if _, err := os.Stat(tlsDirName + "/" + rootKeyCA); !os.IsNotExist(err) {
		return serverKey, serverCert, nil
	}

	// generate self-signed CA and cert for server
	// NOTE: Web-UI user will have to set it as trusted on the desktop

	// prepare hosts
	hostName, err := os.Hostname()
	if err != nil {
		return "", "", err
	}

	hosts := []string{
		hostName,
		hostName + ".local",
	}

	// don't forget localhost for DEV mode
	if logutil.IsDev {
		hosts = append(hosts, "localhost")
	}

	// dates
	notBefore := time.Now()
	notAfter := notBefore.Add(10 * 365 * 24 * time.Hour) // 10 years

	// serial number
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)

	// Root CA
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	if err = savePrivateKey(rootKeyCA, rootKey); err != nil {
		return "", "", err
	}

	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return "", "", err
	}
	rootCert := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"INDIHUB"},
			CommonName:   "Root CA",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootCertBytes, err := x509.CreateCertificate(rand.Reader, &rootCert, &rootCert, &rootKey.PublicKey, rootKey)
	if err != nil {
		return "", "", err
	}
	if err = saveCertificate(rootCertCA, rootCertBytes); err != nil {
		return "", "", err
	}

	// server certificate
	serverPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	if err = savePrivateKey(serverKey, serverPrivateKey); err != nil {
		return "", "", err
	}

	serialNumber, err = rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return "", "", err
	}
	serverCertKey := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"INDIHUB"},
			CommonName:   "indihub-agent",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			serverCertKey.IPAddresses = append(serverCertKey.IPAddresses, ip)
		} else {
			serverCertKey.DNSNames = append(serverCertKey.DNSNames, h)
		}
	}
	serverCertBytes, err := x509.CreateCertificate(rand.Reader, &serverCertKey, &rootCert, &serverPrivateKey.PublicKey, rootKey)
	if err != nil {
		return "", "", err
	}
	if err = saveCertificate(serverCert, serverCertBytes); err != nil {
		return "", "", err
	}

	return serverKey, serverCert, nil
}

func savePrivateKey(fileName string, key *ecdsa.PrivateKey) error {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	err = pem.Encode(
		file,
		&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: data,
		})
	if err != nil {
		return err
	}

	return nil
}

func saveCertificate(fileName string, data []byte) error {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	err = pem.Encode(
		file,
		&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: data,
		})
	if err != nil {
		return err
	}

	return nil
}
