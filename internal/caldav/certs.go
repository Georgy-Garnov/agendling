package caldav

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// verifyRoots are the CAs the system trusts; nil means the system's own (tests replace it).
var verifyRoots *x509.CertPool

// errCertNotTrusted explains a certificate that neither the system nor the account trusts.
var errCertNotTrusted = errors.New("certificate is not the one trusted for this account")

// CertFingerprint is the SHA-256 fingerprint of a certificate, in upper-case hex: the value kept
// in CalendarSource.TrustedCert.
func CertFingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// UntrustedCertificate returns the server certificate behind a failed certificate check, or nil
// when err is not one.
func UntrustedCertificate(err error) *x509.Certificate {
	var ce *tls.CertificateVerificationError
	if errors.As(err, &ce) && len(ce.UnverifiedCertificates) > 0 {
		return ce.UnverifiedCertificates[0]
	}
	return nil
}

// CertInfo describes a server certificate for the user deciding whether to trust it.
type CertInfo struct {
	Subject     string // e.g. the host name it was issued to
	Issuer      string
	SelfSigned  bool
	NotAfter    time.Time
	Fingerprint string // SHA-256, grouped as 4F:9A:...
}

func DescribeCertificate(c *x509.Certificate) CertInfo {
	name := func(n string, fallback string) string {
		if n != "" {
			return n
		}
		return fallback
	}
	subject := c.Subject.CommonName
	if len(c.DNSNames) > 0 && (subject == "" || !contains(c.DNSNames, subject)) {
		subject = strings.Join(c.DNSNames, ", ")
	}
	return CertInfo{
		Subject:     name(subject, c.Subject.String()),
		Issuer:      name(c.Issuer.CommonName, c.Issuer.String()),
		SelfSigned:  c.Issuer.String() == c.Subject.String() && c.CheckSignatureFrom(c) == nil,
		NotAfter:    c.NotAfter,
		Fingerprint: FormatFingerprint(CertFingerprint(c)),
	}
}

// FormatFingerprint groups a hex fingerprint in pairs: 4F:9A:...
func FormatFingerprint(fp string) string {
	var b strings.Builder
	for i := 0; i+1 < len(fp); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(fp[i : i+2])
	}
	return b.String()
}

// ShortFingerprint abbreviates a fingerprint for a one-line mention: 4F:9A:C2:4F … C2:4F.
func ShortFingerprint(fp string) string {
	f := FormatFingerprint(fp)
	if len(f) < 24 {
		return f
	}
	return f[:11] + " … " + f[len(f)-5:]
}

// FingerprintLines splits a grouped SHA-256 fingerprint into two even lines for display.
func FingerprintLines(grouped string) string {
	if len(grouped) != 95 {
		return grouped
	}
	return grouped[:47] + "\n" + grouped[48:]
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// tlsConfigFor checks server certificates as usual and, for an account with a trusted
// certificate, also accepts exactly that certificate. Any other certificate fails with a
// *tls.CertificateVerificationError carrying it, so the user can be asked about it.
func tlsConfigFor(trusted string) *tls.Config {
	if trusted == "" {
		return nil // the system's verification
	}
	return &tls.Config{
		// Verification is done below: the system's, then the trusted certificate.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the server sent no certificate")
			}
			leaf := cs.PeerCertificates[0]
			if strings.EqualFold(CertFingerprint(leaf), trusted) {
				return nil
			}
			opts := x509.VerifyOptions{DNSName: cs.ServerName, Roots: verifyRoots, Intermediates: x509.NewCertPool()}
			for _, c := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(c)
			}
			if _, err := leaf.Verify(opts); err != nil {
				return &tls.CertificateVerificationError{UnverifiedCertificates: cs.PeerCertificates, Err: errCertNotTrusted}
			}
			return nil
		},
	}
}

// syncError rewords errors the user can act on.
func syncError(err error) error {
	if UntrustedCertificate(err) != nil {
		return errors.New("the server's certificate is not trusted (new server, or its certificate changed); " +
			"review it with Test connection in the account's settings")
	}
	return err
}
