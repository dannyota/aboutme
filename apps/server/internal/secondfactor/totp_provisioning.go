package secondfactor

import (
	"errors"
	"net"
	"strings"
)

// TOTP provisioning constants from
// docs/design/totp-second-factor-contract.md "Provisioning data"
// (AC-AUTH-026).
const (
	totpMaxIssuerBytes          = 253
	totpMaxProvisioningURIBytes = 2048
	totpProvisioningLabelPrefix = "otpauth://totp/"
	totpProvisioningQuery       = "algorithm=SHA1&digits=6&period=30"
)

// rfc3986Unreserved holds the RFC 3986 unreserved character set, the only
// bytes percentEncodeRFC3986 leaves unescaped.
const rfc3986Unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// ErrTOTPInvalidIssuer is returned for an issuer that is not a canonical
// lowercase ASCII hostname of 1 to 253 bytes, including an IP literal or a
// non-ASCII host.
var ErrTOTPInvalidIssuer = errors.New("secondfactor: totp issuer is not a canonical ascii hostname")

// ErrTOTPProvisioningURITooLong is returned when the built otpauth URI would
// exceed 2,048 bytes.
var ErrTOTPProvisioningURITooLong = errors.New("secondfactor: totp provisioning uri exceeds the byte bound")

// ValidateTOTPIssuer enforces the issuer bound: 1 to 253 bytes, canonical
// lowercase ASCII, a DNS hostname label shape, and never an IP literal
// ("Provisioning data": "the issuer must be a canonical lowercase ASCII
// hostname").
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

// isASCIIHostname reports whether host is a sequence of 1-to-63-byte labels
// of lowercase ASCII letters, digits, and interior hyphens, separated by
// single dots. A bare label such as "localhost" is accepted, matching local
// HTTPS's issuer ("Provisioning data": "local HTTPS uses localhost").
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

// BuildTOTPProvisioningURI builds the exact otpauth URI from "Provisioning
// data": the escaped issuer and email as separate RFC 3986 percent-encoded
// label components with only their separator colon kept literal, the query
// parameters in the shown order, and space encoded as %20, never +. It
// validates the issuer and rejects a result over 2,048 bytes.
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

// percentEncodeRFC3986 percent-encodes every byte of s outside the RFC 3986
// unreserved set, including a space as %20. It never encodes a space as +.
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
