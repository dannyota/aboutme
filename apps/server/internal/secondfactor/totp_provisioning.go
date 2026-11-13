package secondfactor

import (
	"errors"
	"net"
	"strings"
)

const (
	totpMaxIssuerBytes          = 253
	totpMaxProvisioningURIBytes = 2048
	totpProvisioningLabelPrefix = "otpauth://totp/"
	totpProvisioningQuery       = "algorithm=SHA1&digits=6&period=30"
)

const rfc3986Unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

var ErrTOTPInvalidIssuer = errors.New("secondfactor: totp issuer is not a canonical ascii hostname")

var ErrTOTPProvisioningURITooLong = errors.New("secondfactor: totp provisioning uri exceeds the byte bound")

func ValidateTOTPIssuer(issuer string) error {
	if issuer == "" || len(issuer) > totpMaxIssuerBytes {
		return ErrTOTPInvalidIssuer
	}
	if issuer != strings.ToLower(issuer) {
		return ErrTOTPInvalidIssuer
	}
	if net.ParseIP(issuer) != nil {
		return ErrTOTPInvalidIssuer
	}
	if !isASCIIHostname(issuer) {
		return ErrTOTPInvalidIssuer
	}
	return nil
}

func isASCIIHostname(host string) bool {
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func BuildTOTPProvisioningURI(issuer, email string, secret TOTPSecret) (string, error) {
	if err := ValidateTOTPIssuer(issuer); err != nil {
		return "", err
	}
	escapedIssuer := percentEncodeRFC3986(issuer)
	escapedEmail := percentEncodeRFC3986(email)
	uri := totpProvisioningLabelPrefix + escapedIssuer + ":" + escapedEmail +
		"?secret=" + secret.Base32() +
		"&issuer=" + escapedIssuer + "&" + totpProvisioningQuery
	if len(uri) > totpMaxProvisioningURIBytes {
		return "", ErrTOTPProvisioningURITooLong
	}
	return uri, nil
}

func percentEncodeRFC3986(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(rfc3986Unreserved, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
