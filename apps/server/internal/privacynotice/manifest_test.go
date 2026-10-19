package privacynotice

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestManifestFrozenBytesAndDigest(t *testing.T) {
	manifest := ManifestBytes()
	if len(manifest) != 4683 {
		t.Fatal("manifest byte length differs from independent fixture")
	}
	if hex.EncodeToString(ContentDigest()) != "bb128b39501f09db59915f3f75759572a49349ec78fe17ad48b6ba40d500ffdc" {
		t.Fatal("manifest digest differs from independent fixture")
	}
	if strings.Count(string(manifest), "\n") != 13 || strings.ContainsAny(string(manifest), "\r\x00") || manifest[len(manifest)-1] != '\n' {
		t.Fatal("manifest line framing differs")
	}
	for _, changed := range []string{
		strings.Replace(string(manifest), "2026-10-20", "2026-10-19", 1),
		strings.Replace(string(manifest), "\n", "\r\n", 1),
		strings.Replace(string(manifest), "Việt Nam", "Viet Nam", 1),
		strings.Replace(string(manifest), "public_en=", "public_vi=", 1),
	} {
		if sha256.Sum256([]byte(changed)) == sha256.Sum256(manifest) {
			t.Fatal("changed manifest retained digest")
		}
	}
}
